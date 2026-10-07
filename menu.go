//go:build darwin || linux

package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/crgimenes/glaze"
	"github.com/crgimenes/glaze/menu"
	"github.com/crgimenes/native/openurl"
)

// setMenu installs the native menu bar; where there is none (Linux) it
// returns menu.ErrUnsupported.
func setMenu(w glaze.WebView, b *browser, cfg config) error {
	current := func() string { return b.current }
	f := &finder{w: w}
	z := &zoomer{w: w, i: actualSize}
	_, err := menu.Set([]menu.Item{
		{Title: "Scorcio", Submenu: []menu.Item{
			{Title: "About Scorcio", Selector: "orderFrontStandardAboutPanel:"},
			{Separator: true},
			{Title: "Edit Config…", Shortcut: "cmd+,", OnClick: editConfig},
			{Separator: true},
			{Title: "Services", Services: true},
			{Separator: true},
			{Title: "Hide Scorcio", Shortcut: "cmd+h", Selector: "hide:"},
			{Title: "Hide Others", Shortcut: "cmd+alt+h", Selector: "hideOtherApplications:"},
			{Title: "Show All", Selector: "unhideAllApplications:"},
			{Separator: true},
			{Title: "Quit Scorcio", Shortcut: "cmd+q", OnClick: w.Terminate},
		}},
		{Title: "File", Submenu: []menu.Item{
			{Title: "New Window", Shortcut: "cmd+n", OnClick: func() { newInstance() }},
			{Title: "Open Location…", Shortcut: "cmd+l", OnClick: func() { openLocation(w, current(), b) }},
			{Title: "Open in Browser", Shortcut: "cmd+o", OnClick: func() {
				err := openElsewhere(cfg.Browser, current())
				if err != nil {
					showError("open in browser: " + err.Error())
				}
			}},
			{Separator: true},
			{Title: "Close Window", Shortcut: "cmd+w", OnClick: w.Terminate},
		}},
		{Title: "Edit", Submenu: []menu.Item{
			{Title: "Undo", Shortcut: "cmd+z", Selector: "undo:"},
			{Title: "Redo", Shortcut: "cmd+shift+z", Selector: "redo:"},
			{Separator: true},
			{Title: "Cut", Shortcut: "cmd+x", Selector: "cut:"},
			{Title: "Copy", Shortcut: "cmd+c", Selector: "copy:"},
			{Title: "Paste", Shortcut: "cmd+v", Selector: "paste:"},
			{Title: "Select All", Shortcut: "cmd+a", Selector: "selectAll:"},
			{Separator: true},
			{Title: "Find…", Shortcut: "cmd+f", OnClick: f.ask},
			{Title: "Find Next", Shortcut: "cmd+g", OnClick: func() { f.find(false) }},
			{Title: "Find Previous", Shortcut: "cmd+shift+g", OnClick: func() { f.find(true) }},
		}},
		{Title: "View", Submenu: []menu.Item{
			{Title: "Reload Page", Shortcut: "cmd+r", OnClick: w.Reload},
			{Title: "Stop", Shortcut: "cmd+.", OnClick: func() { w.Stop(); b.settleTitle() }},
			{Separator: true},
			{Title: "Zoom In", Shortcut: "cmd+=", OnClick: func() { z.step(1) }},
			{Title: "Zoom Out", Shortcut: "cmd+-", OnClick: func() { z.step(-1) }},
			{Title: "Actual Size", Shortcut: "cmd+0", OnClick: func() { z.step(actualSize - z.i) }},
			{Separator: true},
			{Title: "Enter Full Screen", Shortcut: "ctrl+cmd+f", Selector: "toggleFullScreen:"},
		}},
		{Title: "Navigate", Submenu: []menu.Item{
			{Title: "Back", Shortcut: "cmd+[", OnClick: w.GoBack},
			{Title: "Forward", Shortcut: "cmd+]", OnClick: w.GoForward},
		}},
		{Title: "Window", Submenu: []menu.Item{
			{Title: "Minimize", Shortcut: "cmd+m", Selector: "performMiniaturize:"},
			{Title: "Zoom", Selector: "performZoom:"},
			{Separator: true},
			{Title: "Bring All to Front", Selector: "arrangeInFront:"},
		}},
	}, menu.Options{Window: w.Window()})
	return err
}

// instanceFlags are passed on to every instance this one starts: a clean
// session's windows are clean too.
var instanceFlags []string

// newInstance starts another scorcio, one page per process: with no url it
// asks for its address. No shell is involved; "--" keeps a url that starts
// with "-" from reading as a flag, and the new process validates it like any
// argument. stdin stays /dev/null so it never reads this one's input.
func newInstance(url ...string) {
	exe, err := os.Executable()
	if err != nil {
		showError("new window: " + err.Error())
		return
	}
	cmd := exec.Command(exe, instanceArgs(url...)...) // #nosec G204 -- our own binary; the url is one argv entry, never a shell word
	cmd.Stderr = os.Stderr
	err = cmd.Start()
	if err != nil {
		showError("new window: " + err.Error())
		return
	}
	go func() { _ = cmd.Wait() }()
}

// openElsewhere hands the page to a full browser: argv from init.filo with
// the url as its last argument, no shell; without one, the system default.
func openElsewhere(argv []string, page string) error {
	u, err := url.Parse(page)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("no web page to open")
	}
	if len(argv) == 0 {
		return openurl.Open(page)
	}
	args := append(append([]string{}, argv[1:]...), page)
	cmd := exec.Command(argv[0], args...) // #nosec G204 -- argv is the user's own init.filo; the url is one argv entry, never a shell word
	cmd.Stderr = os.Stderr
	err = cmd.Start()
	if err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// configTemplate starts an init.filo that Edit Config creates; nothing in it
// runs, so a new file changes nothing until the user writes something.
const configTemplate = `; scorcio reads this file when a window opens; scorcio -h lists what it
; can hold. Examples (remove the ";" to use one):
; (set Search "https://duckduckgo.com/?q=%s")
; (alias "gh" "github.com")
; (site-css "news.ycombinator.com" "body { font-size: 18px }")
`

// editConfig opens init.filo in the default text editor, creating it first.
// Under the sandbox the file lives in the app's container, which is hard to
// find by hand: this is the way to it.
func editConfig() {
	err := ensureConfig(configPath())
	if err == nil {
		err = exec.Command("open", "-t", configPath()).Start() // #nosec G204 -- fixed program, our own config path
	}
	if err != nil {
		showError("edit config: " + err.Error())
	}
}

// ensureConfig writes the template when there is no file; an existing one,
// even empty, is the user's and stays as it is.
func ensureConfig(name string) error {
	if name == "" {
		return errors.New("no config directory")
	}
	err := os.MkdirAll(filepath.Dir(name), 0o700)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- our own config path
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = f.WriteString(configTemplate)
	cerr := f.Close()
	if err != nil {
		return err
	}
	return cerr
}

const addressPrompt = "Address or search"

func openLocation(w glaze.WebView, current string, b *browser) {
	target, err := askFor(addressPrompt, current)
	if err != nil || target == "" {
		return
	}
	page, err := b.resolve(target)
	if err != nil {
		showError(err.Error())
		return
	}
	w.Navigate(page)
}

// finder remembers the last search, so Find Next and Previous repeat it and
// the box opens with it.
type finder struct {
	w    glaze.WebView
	text string
}

func (f *finder) ask() {
	text, err := askFor("Find in page", f.text)
	if err != nil || text == "" {
		f.w.Find("", false, nil) // cancelled: clear the highlights
		return
	}
	f.text = text
	f.find(false)
}

func (f *finder) find(backwards bool) {
	if f.text == "" {
		f.ask()
		return
	}
	text := f.text
	f.w.Find(text, backwards, func(found bool) {
		if !found {
			// Out of the engine's callback before a modal alert.
			f.w.Dispatch(func() { showError(fmt.Sprintf("%q not found", text)) })
		}
	})
}

// zoomSteps are the levels Zoom In and Out walk through, as in Safari.
var zoomSteps = []float64{0.5, 0.67, 0.75, 0.8, 0.9, 1, 1.1, 1.25, 1.5, 1.75, 2, 2.5, 3}

const actualSize = 5 // zoomSteps[5] == 1

type zoomer struct {
	w glaze.WebView
	i int
}

func (z *zoomer) step(n int) {
	z.i = min(max(z.i+n, 0), len(zoomSteps)-1)
	z.w.SetZoom(zoomSteps[z.i])
}

// instanceArgs is the command line of a new instance: this one's inherited
// flags, then the url after "--" so it can never read as a flag.
func instanceArgs(url ...string) []string {
	args := append([]string{}, instanceFlags...)
	if len(url) > 0 {
		args = append(args, "--", url[0])
	}
	return args
}
