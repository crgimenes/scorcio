//go:build darwin || linux

package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/crgimenes/glaze"
	"github.com/crgimenes/native/alert"
)

// runner is a WebView that runs what is dispatched to it.
type runner struct{ titleRecorder }

func (r *runner) Dispatch(f func()) { f() }

// fakeDialogs answers each dialog with the next button title in answers (the
// text field gets text) and records what was shown.
func fakeDialogs(t *testing.T, answers []string, text string) *[]string {
	t.Helper()
	shown := &[]string{}
	old := showDialog
	t.Cleanup(func() { showDialog = old })
	showDialog = func(o alert.Options) (alert.Result, error) {
		var titles []string
		for _, b := range o.Buttons {
			titles = append(titles, b.Title)
		}
		*shown = append(*shown, o.Title+": "+o.Message+" ["+strings.Join(titles, ",")+"]")
		if len(answers) == 0 {
			return alert.Result{}, errors.New("no answer left")
		}
		a := answers[0]
		answers = answers[1:]
		for i, ti := range titles {
			if ti == a {
				return alert.Result{Button: i, Text: text}, nil
			}
		}
		t.Fatalf("no button %q in %v", a, titles)
		return alert.Result{}, nil
	}
	return shown
}

// A page's dialogs carry its site's name, answer as chosen, and from the
// second one offer Stop Dialogs, which silences the page until a new document
// commits.
func TestScriptDialogs(t *testing.T) {
	shown := fakeDialogs(t, []string{"OK", "OK", "Cancel", stopDialogs, "OK"}, "typed")
	b := &browser{w: &runner{}, trace: func(string, ...any) {}, current: "https://a.example/x"}
	answer := func(d glaze.ScriptDialog) string {
		ok, text := b.scriptDialog(d)
		return strings.TrimSpace(strings.Join([]string{map[bool]string{true: "ok", false: "no"}[ok], text}, " "))
	}
	got := []string{
		answer(glaze.ScriptDialog{Kind: glaze.ScriptAlert, Message: "hi"}),
		answer(glaze.ScriptDialog{Kind: glaze.ScriptPrompt, Message: "name?", Text: "def"}),
		answer(glaze.ScriptDialog{Kind: glaze.ScriptConfirm, Message: "sure?"}),
		answer(glaze.ScriptDialog{Kind: glaze.ScriptConfirm, Message: "again?"}),
		answer(glaze.ScriptDialog{Kind: glaze.ScriptAlert, Message: "silenced"}),
	}
	b.loading = true
	b.urlChange("https://b.example/") // a new document
	got = append(got, answer(glaze.ScriptDialog{Kind: glaze.ScriptAlert, Message: "new page"}))

	want := "ok typed | ok typed | no typed | no | no | ok typed"
	if strings.Join(got, " | ") != want {
		t.Fatalf("answers %q, want %q", strings.Join(got, " | "), want)
	}
	wantShown := []string{
		"a.example says: hi [OK]",
		"a.example says: name? [OK,Cancel," + stopDialogs + "]",
		"a.example says: sure? [OK,Cancel," + stopDialogs + "]",
		"a.example says: again? [OK,Cancel," + stopDialogs + "]",
		"b.example says: new page [OK]",
	}
	if strings.Join(*shown, "\n") != strings.Join(wantShown, "\n") {
		t.Fatalf("shown:\n%s\nwant:\n%s", strings.Join(*shown, "\n"), strings.Join(wantShown, "\n"))
	}
}

// A link to another app is offered to the system after asking; a scheme that
// is not another app's (file: above all) is not, and stays a failure.
func TestExternalLinks(t *testing.T) {
	shown := fakeDialogs(t, []string{"Open", "Cancel"}, "")
	var opened []string
	old := openExternal
	t.Cleanup(func() { openExternal = old })
	openExternal = func(u string) error { opened = append(opened, u); return nil }
	b := &browser{w: &runner{}, trace: func(string, ...any) {}, current: "https://a.example/"}

	for _, u := range []string{"mailto:x@example.com", "zoommtg://join?x=1"} {
		if !b.external(u) {
			t.Fatalf("%s not offered", u)
		}
	}
	for _, u := range []string{"file:///Applications/Calculator.app", "javascript:alert(1)", "https://a.example/", "data:text/html,x", "about:blank", "", "::"} {
		if b.external(u) {
			t.Fatalf("%q offered to another app", u)
		}
	}
	if strings.Join(opened, " ") != "mailto:x@example.com" {
		t.Fatalf("opened %v", opened)
	}
	if len(*shown) != 2 || !strings.HasPrefix((*shown)[0], "a.example wants to open another app: mailto:x@example.com [Open,Cancel]") {
		t.Fatalf("shown %q", *shown)
	}
}

// A page's new window opens a new scorcio only for a web page; a link to
// another app goes to it after asking, and a blank window is dropped rather
// than becoming a search for "about:blank" or "mailto:...".
func TestNewWindowRouting(t *testing.T) {
	fakeDialogs(t, []string{"Open"}, "")
	var spawned, opened []string
	oldSpawn, oldOpen := spawn, openExternal
	t.Cleanup(func() { spawn, openExternal = oldSpawn, oldOpen })
	spawn = func(u string) { spawned = append(spawned, u) }
	openExternal = func(u string) error { opened = append(opened, u); return nil }
	b := &browser{w: &runner{}, trace: func(string, ...any) {}, current: "https://a.example/"}

	for _, u := range []string{"https://b.example/", "mailto:x@example.com", "about:blank", "", "javascript:void(0)", "http://"} {
		b.newWindow(u)
	}
	if strings.Join(spawned, " ") != "https://b.example/" {
		t.Fatalf("spawned %q", spawned)
	}
	if strings.Join(opened, " ") != "mailto:x@example.com" {
		t.Fatalf("opened %q", opened)
	}
}
