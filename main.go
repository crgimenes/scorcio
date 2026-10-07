//go:build darwin || linux

package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/crgimenes/filo"
	"github.com/crgimenes/glaze"
	"github.com/crgimenes/native/alert"
)

var Version = "dev"

func init() { runtime.LockOSThread() }

const usage = `usage: scorcio [url | -]

Open one page in one window. Without a url, ask for it; with "-" or a
pipe on stdin, read it from the first line of stdin.

  -h, --help   show this help
  -version     print the version
  -trace       print timings to stderr (ms since start): window; start of a
               navigation; url, at its commit or a same-page move; shown,
               when the page first appears; load, when every resource is
               in; fail
  -clean       a clean session: nothing saved by earlier runs is read, and
               nothing (cookies, storage, cache) is kept; windows opened
               from it are clean too

config: scorcio/init.filo in the platform config dir (on macOS,
~/Library/Application Support); optional. Search is the provider,
%s marks the query:
  (set Search "https://duckduckgo.com/?q=%s")
Browser is what Open in Browser (cmd+o) runs, the url appended; empty
is the system default:
  (set Browser (list "open" "-a" "Firefox"))
alias names an address, or with %s a search for the rest of the line:
  (alias "gh" "github.com")
  (alias "w" "https://en.wikipedia.org/wiki/Special:Search?search=%s")
site-css styles a host and its subdomains ("*" is every site) before
the page first paints:
  (site-css "news.ycombinator.com" "body { font-size: 18px }")
an on-load function runs after each page loads, and may call
(navigate "url"), (reload) and (inject-css "css"):
  (def on-load (fn (url) (inject-css "body { font-size: 18px }")))

example:
  scorcio "$(fzf < ~/.config/browser/bookmarks)"
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin))
}

func run(args []string, stdin *os.File) int {
	start := time.Now()
	fs := flag.NewFlagSet("scorcio", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	showVersion := fs.Bool("version", false, "")
	trace := fs.Bool("trace", false, "")
	clean := fs.Bool("clean", false, "")
	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		_, _ = io.WriteString(os.Stdout, usage)
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "scorcio:", err)
		_, _ = io.WriteString(os.Stderr, usage)
		return 2
	}
	if *showVersion {
		fmt.Println(Version)
		return 0
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "scorcio: one page per window: give at most one url")
		return 2
	}

	target := fs.Arg(0)
	if target == "-" || (target == "" && !isTerminal(stdin)) {
		target, err = firstLine(stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, "scorcio: reading stdin:", err)
			return 1
		}
	}

	tracef := func(format string, a ...any) {
		if *trace {
			ms := float64(time.Since(start).Microseconds()) / 1000
			fmt.Fprintf(os.Stderr, "scorcio: t=%.1fms "+format+"\n", append([]any{ms}, a...)...)
		}
	}
	b := &browser{trace: tracef}
	if *clean {
		instanceFlags = []string{"-clean"}
	}
	cfg, err := loadConfig(configPath(), b.registerPrimitives)
	tracef("event=config script=%v", cfg.script != nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "scorcio:", err)
		return 2
	}
	b.search, b.script = cfg.Search, cfg.script

	page := ""
	if target != "" {
		page, err = b.resolve(target)
		if err != nil {
			fmt.Fprintln(os.Stderr, "scorcio:", err)
			return 2
		}
	}

	var w glaze.WebView
	w, err = glaze.NewWithOptions(glaze.Options{
		NoBridge:          true,
		HideUntilLoaded:   true,
		Ephemeral:         *clean,
		OnNavigation:      b.navigation,
		OnNavigationStart: b.navigationStart,
		OnURLChange:       b.urlChange,
		OnContentShown:    b.contentShown,
		OnNewWindow:       b.newWindow,
		OnOpenURLs:        b.openURLs,
		OnDownload:        b.download,
		OnDownloadDone:    b.downloadDone,
		OnMediaCapture:    b.mediaCapture,
		OnScriptDialog:    b.scriptDialog,
		FrameAutosaveName: "scorcio",
		// A browser: two fingers sideways go back and forward, as in Safari.
		NavigationGestures: true,
		// Pages from anywhere: on Linux with GTK3 their process runs in
		// WebKitGTK's bubblewrap sandbox, as it always does with GTK4.
		SandboxWebContent: true,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "scorcio:", err)
		return 1
	}
	defer w.Destroy()
	b.w = w
	w.SetSize(1024, 768, glaze.HintNone)
	b.initStyles()
	err = setMenu(w, b, cfg)
	if err != nil {
		tracef("event=menu err=%q", err)
	}
	tracef("event=window")

	page, err = b.firstPage(page)
	if err != nil {
		fmt.Fprintln(os.Stderr, "scorcio:", err)
		return 2
	}
	if page == "" {
		code := 0
		page, code = askPage(b)
		if page == "" {
			return code
		}
	}

	w.SetTitle(page)
	w.Navigate(page)
	w.Run()
	return b.exitCode
}

func failureMessage(ev glaze.NavigationEvent) string {
	if ev.TLS {
		return fmt.Sprintf("%s: insecure connection, not loaded: %v", ev.URL, ev.Err)
	}
	return fmt.Sprintf("%s: %v", ev.URL, ev.Err)
}

// Launched from Finder or the Dock, stderr is /dev/null and nobody would
// read the message there.
func stderrDiscarded() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return true
	}
	null, err := os.Stat(os.DevNull)
	return err == nil && os.SameFile(fi, null)
}

func showError(msg string) {
	_, _ = alert.Show(alert.Options{
		Title:   "Scorcio",
		Message: msg,
		Buttons: []alert.Button{{Title: "OK", Cancel: true}},
	})
}

// Launched from Finder or the Dock, stdin is /dev/null: a character device,
// so it counts as a terminal and the address box opens.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return true
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func firstLine(r io.Reader) (string, error) {
	s := bufio.NewScanner(r)
	if s.Scan() {
		return strings.TrimSpace(strings.TrimPrefix(s.Text(), "\ufeff")), nil
	}
	return "", s.Err()
}

// askPage asks for the first page. Cancelling ends the process (code 0):
// there is no page to stay on.
func askPage(b *browser) (string, int) {
	target, err := askFor(addressPrompt, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "scorcio:", err)
		return "", 1
	}
	if target == "" {
		return "", 0
	}
	page, err := b.resolve(target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "scorcio:", err)
		return "", 2
	}
	return page, 0
}

// askFor shows the text box with message, starting with text; "" means
// cancelled.
func askFor(message, text string) (string, error) {
	res, err := alert.Show(alert.Options{
		Title:   "Scorcio",
		Message: message,
		Input:   true,
		Text:    text,
		Buttons: []alert.Button{{Title: "OK"}, {Title: "Cancel", Cancel: true}},
	})
	if errors.Is(err, alert.ErrUnsupported) {
		return "", errors.New("no url given, and this platform has no address box yet")
	}
	if err != nil {
		return "", err
	}
	if res.Button != 0 {
		return "", nil
	}
	return strings.TrimSpace(res.Text), nil
}

// browser answers what the page asks of the process.
type browser struct {
	w       glaze.WebView
	trace   func(format string, a ...any)
	search  string
	script  *filo.Filo
	styles  []siteStyle
	aliases map[string]string

	// URLs the system asked this process to open before its first page was
	// chosen: it was launched to open a link.
	pending []string
	started bool

	current  string // the page on screen, once one committed
	loading  bool   // a navigation started and has not ended
	exitCode int

	// media holds the answer per origin, so a page that asks again does not
	// bring the question back for the rest of this process.
	media map[mediaRequest]bool

	// dialogs counts the dialogs of the document on screen; dialogsStopped
	// is its Stop Dialogs. See scriptDialog.
	dialogs        int
	dialogsStopped bool
}

type mediaRequest struct {
	origin             string
	camera, microphone bool
}

// ask shows a yes/no question; tests swap it out.
var ask = func(message, yes, no string) bool {
	res, err := alert.Show(alert.Options{
		Title:   "Scorcio",
		Message: message,
		Buttons: []alert.Button{{Title: yes}, {Title: no, Cancel: true}},
	})
	return err == nil && res.Button == 0
}

// mediaCapture asks before a page gets the camera or microphone, as a
// browser does for a call.
func (b *browser) mediaCapture(origin string, camera, microphone bool) bool {
	req := mediaRequest{origin, camera, microphone}
	allow, asked := b.media[req]
	if asked {
		return allow
	}
	what := "camera and microphone"
	switch {
	case !microphone:
		what = "camera"
	case !camera:
		what = "microphone"
	}
	allow = ask(fmt.Sprintf("%s wants to use your %s.", origin, what), "Allow", "Don't Allow")
	b.trace("event=media origin=%q camera=%v microphone=%v allow=%v", origin, camera, microphone, allow)
	if b.media == nil {
		b.media = map[mediaRequest]bool{}
	}
	b.media[req] = allow
	return allow
}

// loadingTitle marks the window title while a page loads: the engine keeps
// the old page on screen until the new one draws, so without it a slow link
// looks like a click that did nothing.
func loadingTitle(url string) string { return "… " + url }

func (b *browser) navigationStart(url string) {
	b.loading = true
	b.w.SetTitle(loadingTitle(url))
	b.trace("event=start url=%q", url)
}

// urlChange follows the page on screen: a navigation's document at its
// commit, while its resources may still load (so the title keeps the loading
// mark), or a page that moves its URL without loading. Neither is the end of
// a load, so on-load does not run.
func (b *browser) urlChange(url string) {
	b.current = url
	title := url
	if b.loading {
		// A new document: its dialogs start over. Not at the start, which a
		// page looping on mailto: would reach every time.
		b.dialogs, b.dialogsStopped = 0, false
		title = loadingTitle(url)
	}
	b.w.SetTitle(title)
	b.trace("event=url url=%q", url)
}

func (b *browser) contentShown() { b.trace("event=shown") }

func (b *browser) navigation(ev glaze.NavigationEvent) {
	b.loading = false
	if ev.Kind == glaze.NavigationFinished {
		b.current = ev.URL
		b.w.SetTitle(ev.URL)
		b.trace("event=load url=%q", ev.URL)
		b.loaded(ev.URL)
		return
	}
	if b.external(ev.URL) {
		b.settleTitle()
		return
	}
	b.trace("event=fail url=%q tls=%v err=%q", ev.URL, ev.TLS, ev.Err)
	msg := failureMessage(ev)
	fmt.Fprintln(os.Stderr, "scorcio:", msg)
	if b.current != "" {
		// The engine keeps the current page; only tell the user.
		b.settleTitle()
		b.w.Dispatch(func() { showError(msg) })
		return
	}
	// The first page did not open: nothing to show, so leave.
	b.exitCode = 1
	b.w.Dispatch(func() {
		if stderrDiscarded() {
			showError(msg)
		}
		b.w.Terminate()
	})
}

// settleTitle ends the loading mark for a navigation that stopped without
// finishing: cancelled with Stop, or turned into a download.
func (b *browser) settleTitle() {
	b.loading = false
	if b.current != "" {
		b.w.SetTitle(b.current)
	}
}

// resolve is the address rules with init.filo's aliases.
func (b *browser) resolve(input string) (string, error) {
	return resolve(expandAlias(input, b.aliases), b.search)
}

// spawn starts another instance for a URL; tests swap it out.
var spawn = func(url string) { newInstance(url) }

// openURLs answers a link another app opened with scorcio. One page per
// process: the first URL of a launch becomes this window's page, every
// other one a new instance.
func (b *browser) openURLs(urls []string) {
	b.trace("event=openurls urls=%q", urls)
	if !b.started {
		b.pending = append(b.pending, urls...)
		return
	}
	for _, u := range urls {
		spawn(u)
	}
}

// firstPage settles this window's page: the command line's, else the first
// URL the system launched it with; the other URLs get instances of their own.
func (b *browser) firstPage(page string) (string, error) {
	b.started = true
	rest := b.pending
	b.pending = nil
	if page == "" && len(rest) > 0 {
		var err error
		page, err = b.resolve(rest[0])
		rest = rest[1:]
		if err != nil {
			return "", err
		}
	}
	for _, u := range rest {
		spawn(u)
	}
	return page, nil
}

// A page's new window is a new scorcio: one page, one process.
// A link to another app goes to it (after asking) instead: a new scorcio
// would take mailto:x@y for a search. A blank window (window.open() with no
// address, about:blank) is dropped: the page that would fill it lives in
// another process.
func (b *browser) newWindow(url string) {
	b.trace("event=newwindow url=%q", url)
	switch {
	case isWebURL(url):
		spawn(url)
	case b.external(url):
	default:
		b.trace("event=newwindow-dropped url=%q", url)
	}
}

// download always asks where: nothing is saved on the page's say-so.
func (b *browser) download(name string) string {
	b.trace("event=download name=%q", name)
	b.settleTitle()
	path, err := b.w.SaveFile(glaze.FileDialogOptions{Filename: name, Directory: downloadsDir()})
	if err != nil {
		fmt.Fprintln(os.Stderr, "scorcio: download:", err)
	}
	return path
}

func (b *browser) downloadDone(path string, err error) {
	if err != nil {
		msg := fmt.Sprintf("%s: download failed: %v", path, err)
		fmt.Fprintln(os.Stderr, "scorcio:", msg)
		b.w.Dispatch(func() { showError(msg) })
		return
	}
	fmt.Fprintln(os.Stderr, "scorcio: saved", path)
}

// downloadsDir is where the save panel starts: ~/Downloads when there is one.
// Under the sandbox home is the app container, whose Downloads is a symlink to
// the real folder; Lstat looks at the link, which the sandbox lets the app
// see, and the panel -- outside the sandbox -- follows it. "" leaves the
// choice to the panel.
func downloadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, "Downloads")
	_, err = os.Lstat(dir)
	if err != nil {
		return ""
	}
	return dir
}
