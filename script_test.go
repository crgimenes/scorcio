//go:build darwin || linux

package main

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/crgimenes/filo"
	"github.com/crgimenes/glaze"
)

type pageRecorder struct {
	glaze.WebView
	calls []string
}

func (r *pageRecorder) Navigate(url string) { r.calls = append(r.calls, "navigate "+url) }
func (r *pageRecorder) Reload()             { r.calls = append(r.calls, "reload") }
func (r *pageRecorder) Eval(js string)      { r.calls = append(r.calls, "eval "+js) }

func loadScript(t *testing.T, src string) (*browser, *pageRecorder, error) {
	t.Helper()
	b := &browser{trace: func(string, ...any) {}}
	cfg, err := loadConfig(writeConfig(t, src), b.registerPrimitives)
	r := &pageRecorder{}
	b.w, b.search, b.script = r, cfg.Search, cfg.script
	return b, r, err
}

func TestOnLoadCallsPrimitives(t *testing.T) {
	b, r, err := loadScript(t, `
(def on-load (fn (url)
  (if (str-find "news.example" url)
    (do (inject-css "a::after { content: \"</style>\" }")
        (navigate "example.org/next")))))`)
	if err != nil {
		t.Fatal(err)
	}
	b.loaded("https://other.example/")
	if len(r.calls) != 0 {
		t.Fatalf("hook acted on a page it does not match: %q", r.calls)
	}
	b.loaded("https://news.example/")
	want := []string{
		`eval (function(c){` + applyCSS + `})("a::after { content: \"\u003c/style\u003e\" }")`,
		"navigate https://example.org/next",
	}
	if strings.Join(r.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(r.calls, "\n"), strings.Join(want, "\n"))
	}
}

func TestNavigateKeepsTheAddressRules(t *testing.T) {
	b, r, err := loadScript(t, `(def on-load (fn (url) (navigate "file:///etc/passwd")))`)
	if err != nil {
		t.Fatal(err)
	}
	b.loaded("https://x.example/")
	if len(r.calls) != 0 {
		t.Fatalf("navigate loaded a non-http url: %q", r.calls)
	}
}

func TestPrimitiveWhileLoadingIsAnError(t *testing.T) {
	b := &browser{trace: func(string, ...any) {}}
	_, err := loadConfig(writeConfig(t, `(reload)`), b.registerPrimitives)
	if err == nil || !errors.Is(err, errNoPage) && !strings.Contains(err.Error(), errNoPage.Error()) {
		t.Fatalf("err = %v, want %v", err, errNoPage)
	}
}

func TestRunawayHookIsBounded(t *testing.T) {
	b, _, err := loadScript(t, `(def spin (fn (n) (spin (+ n 1))))
(def on-load (fn (url) (spin 0)))`)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	b.loaded("https://x.example/")
	if d := time.Since(start); d > time.Second {
		t.Fatalf("a runaway on-load held the UI thread for %v", d)
	}
}

func TestNoScriptNoHook(t *testing.T) {
	b, r, err := loadScript(t, `(set Search "https://s.example/?q=%s")`)
	if err != nil {
		t.Fatal(err)
	}
	b.loaded("https://x.example/")
	if len(r.calls) != 0 {
		t.Fatalf("calls without an on-load: %q", r.calls)
	}
}

const typicalScript = `(set Search "https://duckduckgo.com/?q=%s")
(def on-load (fn (url)
  (if (str-find "news.ycombinator.com" url)
    (inject-css "body { font-size: 18px }"))))`

func BenchmarkLoadConfig(b *testing.B) {
	name := writeConfig(b, typicalScript)
	br := &browser{trace: func(string, ...any) {}}
	for b.Loop() {
		_, _ = loadConfig(name, br.registerPrimitives)
	}
}

func BenchmarkOnLoad(b *testing.B) {
	br := &browser{trace: func(string, ...any) {}}
	cfg, err := loadConfig(writeConfig(b, typicalScript), br.registerPrimitives)
	if err != nil {
		b.Fatal(err)
	}
	br.w, br.search, br.script = &pageRecorder{}, cfg.Search, cfg.script
	for b.Loop() {
		br.loaded("https://news.ycombinator.com/")
	}
}

func TestSiteCSS(t *testing.T) {
	b, r, err := loadScript(t, `(site-css "News.Example" "body { color: red }")
(site-css "*" "a { color: blue }")`)
	if err != nil {
		t.Fatal(err)
	}
	want := []siteStyle{{"news.example", "body { color: red }"}, {"*", "a { color: blue }"}}
	if len(b.styles) != len(want) || b.styles[0] != want[0] || b.styles[1] != want[1] {
		t.Fatalf("styles = %q, want %q", b.styles, want)
	}

	_, err = b.primSiteCSS(t.Context(), []filo.Value{filo.VString("x.example"), filo.VString("p {}")})
	if err == nil {
		t.Error("site-css from a hook: want error")
	}
	if len(r.calls) != 0 {
		t.Errorf("site-css touched the page: %q", r.calls)
	}

	for _, src := range []string{
		`(site-css "https://x.example" "p {}")`,
		`(site-css "x.example/path" "p {}")`,
		`(site-css "" "p {}")`,
		`(site-css "x.example")`,
		`(site-css "x.example" 1)`,
	} {
		_, _, err := loadScript(t, src)
		if err == nil {
			t.Errorf("%s: want error", src)
		}
	}
}

func TestStyleScriptWithoutRules(t *testing.T) {
	js := styleScript(nil)
	if js != "" {
		t.Fatalf("styleScript(nil) = %q", js)
	}
}

// TestStyleScriptMatchesHosts runs the document-start script in node with a
// stand-in document; skipped without node.
func TestStyleScriptMatchesHosts(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	js := styleScript([]siteStyle{{"news.example", "a{}"}, {"*", "b{}"}, {"other.example", "c{}"}})
	stub := `class CSSStyleSheet{replaceSync(c){this.c=c}}
globalThis.CSSStyleSheet=CSSStyleSheet;
globalThis.document={adoptedStyleSheets:[]};
for (const h of process.argv.slice(1)) {
  globalThis.location={hostname:h};document.adoptedStyleSheets=[];
  ` + js + `;
  console.log(h+"="+document.adoptedStyleSheets.map(s=>s.c.trim().replace(/\n/g," ")).join("|"));
}`
	out, err := exec.Command(node, "-e", stub, "news.example", "www.news.example", "evilnews.example", "x.test").CombinedOutput() // #nosec G204 -- test-built script
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := "news.example=a{} b{}\nwww.news.example=a{} b{}\nevilnews.example=b{}\nx.test=b{}\n"
	if string(out) != want {
		t.Fatalf("node output:\n%s\nwant:\n%s", out, want)
	}
}

func TestAlias(t *testing.T) {
	b, _, err := loadScript(t, `(alias "gh" "github.com")
(alias "w" "https://en.wikipedia.org/wiki/Special:Search?search=%s")
(alias "dev" "localhost:8080/admin")`)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ in, want string }{
		{"gh", "https://github.com/"},
		{"  gh  ", "https://github.com/"},
		{"w go & rust", "https://en.wikipedia.org/wiki/Special:Search?search=go+%26+rust"},
		{"dev", "http://localhost:8080/admin"},
		{"gh issues", "https://duckduckgo.com/?q=gh+issues"}, // no %s: the alias alone
		{"GH", "https://duckduckgo.com/?q=GH"},
		{"github.com", "https://github.com/"},
		{"https://w.example/", "https://w.example/"},
	}
	for _, c := range cases {
		got, err := b.resolve(c.in)
		if err != nil || got != c.want {
			t.Errorf("resolve(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}

	for _, src := range []string{
		`(alias "x" "file:///etc/passwd")`,
		`(alias "x" "javascript:alert(1)")`,
		`(alias "x" "http://s.example/?q=%s")`,
		`(alias "x" "https://s.example/?q=%s&r=%s")`,
		`(alias "x" "two words")`,
		`(alias "x" "see https://go.dev")`,
		`(alias "two words" "example.com")`,
		`(alias "" "example.com")`,
		`(alias "x")`,
	} {
		_, _, err := loadScript(t, src)
		if err == nil {
			t.Errorf("%s: want error", src)
		}
	}
}
