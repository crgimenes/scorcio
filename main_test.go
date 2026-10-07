//go:build darwin || linux

package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/crgimenes/glaze"
)

func TestRunExitCodes(t *testing.T) {
	cases := []struct {
		args []string
		want int
	}{
		{[]string{"-h"}, 0},
		{[]string{"--help"}, 0},
		{[]string{"-version"}, 0},
		{[]string{"-nope"}, 2},
		{[]string{"a", "b"}, 2},
	}
	for _, c := range cases {
		got := run(c.args, os.Stdin)
		if got != c.want {
			t.Errorf("run(%q) = %d, want %d", c.args, got, c.want)
		}
	}
}

func TestFirstLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://go.dev\nignored\n", "https://go.dev"},
		{"\ufeffexample.com\r\n", "example.com"},
		{"  spaced  ", "spaced"},
		{"", ""},
	}
	for _, c := range cases {
		got, err := firstLine(strings.NewReader(c.in))
		if err != nil {
			t.Fatalf("firstLine(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("firstLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestOpenURLsOnePagePerProcess(t *testing.T) {
	var spawned []string
	orig := spawn
	spawn = func(u string) { spawned = append(spawned, u) }
	t.Cleanup(func() { spawn = orig })

	b := &browser{trace: func(string, ...any) {}, search: defaultSearch}
	// Launched to open two links: the first becomes this window's page.
	b.openURLs([]string{"https://a.example/", "https://b.example/"})
	page, err := b.firstPage("")
	if err != nil || page != "https://a.example/" {
		t.Fatalf("firstPage = %q, %v; want the first launch URL", page, err)
	}
	// Once running, every link is a new instance.
	b.openURLs([]string{"https://c.example/"})
	if got := strings.Join(spawned, " "); got != "https://b.example/ https://c.example/" {
		t.Fatalf("spawned %q", got)
	}

	// A command-line page wins; launch URLs all get instances.
	spawned = nil
	b = &browser{trace: func(string, ...any) {}, search: defaultSearch}
	b.openURLs([]string{"https://d.example/"})
	page, _ = b.firstPage("https://cli.example/")
	if page != "https://cli.example/" || strings.Join(spawned, " ") != "https://d.example/" {
		t.Fatalf("page %q spawned %q", page, spawned)
	}

	// A launch URL that is not http(s) is refused like any other address.
	b = &browser{trace: func(string, ...any) {}, search: defaultSearch}
	b.openURLs([]string{"file:///etc/passwd"})
	_, err = b.firstPage("")
	if err == nil {
		t.Fatal("a file: launch URL was accepted")
	}
}

type titleRecorder struct {
	glaze.WebView
	titles     []string
	dispatched int
}

func (r *titleRecorder) SetTitle(t string) { r.titles = append(r.titles, t) }
func (r *titleRecorder) Dispatch(f func()) { r.dispatched++ } // never runs the alert

// The title marks a page while it loads and goes back to the page on screen
// when the load ends any other way than finishing.
func TestLoadingTitle(t *testing.T) {
	r := &titleRecorder{}
	b := &browser{w: r, trace: func(string, ...any) {}}
	nav := func(kind glaze.NavigationKind, url string) {
		ev := glaze.NavigationEvent{Kind: kind, URL: url}
		if kind == glaze.NavigationFailed {
			ev.Err = errors.New("refused")
		}
		b.navigation(ev)
	}
	b.navigationStart("https://a.example/")
	nav(glaze.NavigationFinished, "https://a.example/")
	b.navigationStart("https://b.example/")
	nav(glaze.NavigationFailed, "https://b.example/")
	b.navigationStart("https://c.example/file.zip")
	b.settleTitle() // what Stop and a download do
	want := []string{
		"… https://a.example/", "https://a.example/",
		"… https://b.example/", "https://a.example/",
		"… https://c.example/file.zip", "https://a.example/",
	}
	if strings.Join(r.titles, " | ") != strings.Join(want, " | ") {
		t.Fatalf("titles:\n%s\nwant:\n%s", strings.Join(r.titles, " | "), strings.Join(want, " | "))
	}
	if b.exitCode != 0 || r.dispatched != 1 {
		t.Fatalf("a failure after the first page: exit %d, %d dispatches; want 0 and 1 (the alert)", b.exitCode, r.dispatched)
	}
}

// A URL moved by the page itself becomes the page on screen, what the title,
// Open Location and Open in Browser use, without counting as a load.
func TestURLChange(t *testing.T) {
	r := &titleRecorder{}
	b := &browser{w: r, trace: func(string, ...any) {}}
	b.navigation(glaze.NavigationEvent{Kind: glaze.NavigationFinished, URL: "https://a.example/"})
	b.urlChange("https://a.example/b#section")
	if b.current != "https://a.example/b#section" {
		t.Fatalf("current = %q", b.current)
	}
	if got := strings.Join(r.titles, " | "); got != "https://a.example/ | https://a.example/b#section" {
		t.Fatalf("titles %q", got)
	}
}

// The URL the actions use follows a navigation from its commit, before every
// resource is in; a navigation that fails before committing, or a Stop, keeps
// the page on screen and its URL.
func TestURLAtCommit(t *testing.T) {
	r := &titleRecorder{}
	b := &browser{w: r, trace: func(string, ...any) {}}
	b.navigationStart("https://a.example/start")
	b.urlChange("https://a.example/page") // committed after a redirect
	if b.current != "https://a.example/page" {
		t.Fatalf("current at commit = %q", b.current)
	}
	b.navigation(glaze.NavigationEvent{Kind: glaze.NavigationFinished, URL: "https://a.example/page"})
	b.navigationStart("https://bad.example/")
	b.navigation(glaze.NavigationEvent{Kind: glaze.NavigationFailed, URL: "https://bad.example/", Err: errors.New("tls"), TLS: true})
	if b.current != "https://a.example/page" {
		t.Fatalf("current after a provisional failure = %q", b.current)
	}
	b.navigationStart("https://c.example/")
	b.urlChange("https://c.example/")
	b.settleTitle() // Stop after the commit: c stays on screen
	if b.current != "https://c.example/" {
		t.Fatalf("current after Stop = %q", b.current)
	}
	want := []string{
		"… https://a.example/start", "… https://a.example/page", "https://a.example/page",
		"… https://bad.example/", "https://a.example/page",
		"… https://c.example/", "… https://c.example/", "https://c.example/",
	}
	if strings.Join(r.titles, " | ") != strings.Join(want, " | ") {
		t.Fatalf("titles:\n%s\nwant:\n%s", strings.Join(r.titles, " | "), strings.Join(want, " | "))
	}
}

// A site is asked about once per process for each kind of request, and a
// refusal is remembered like a grant.
func TestMediaCaptureAsksOnce(t *testing.T) {
	var questions []string
	answer := true
	defer func(f func(string, string, string) bool) { ask = f }(ask)
	ask = func(message, yes, no string) bool {
		questions = append(questions, message)
		return answer
	}
	b := &browser{trace: func(string, ...any) {}}
	for range 2 {
		if !b.mediaCapture("https://meet.example", true, true) {
			t.Fatal("granted call not allowed")
		}
	}
	answer = false
	for range 2 {
		if b.mediaCapture("https://other.example", false, true) {
			t.Fatal("refused microphone allowed")
		}
	}
	if b.mediaCapture("https://meet.example", true, false) {
		t.Fatal("a camera-only request reused the camera+microphone answer without asking")
	}
	want := []string{
		"https://meet.example wants to use your camera and microphone.",
		"https://other.example wants to use your microphone.",
		"https://meet.example wants to use your camera.",
	}
	if strings.Join(questions, "\n") != strings.Join(want, "\n") {
		t.Fatalf("questions:\n%s\nwant:\n%s", strings.Join(questions, "\n"), strings.Join(want, "\n"))
	}
}
