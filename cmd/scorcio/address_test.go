//go:build darwin || linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	const s = "https://search.example/?q=%s"
	cases := []struct {
		in, want string
	}{
		{"https://go.dev", "https://go.dev/"},
		{"http://go.dev/doc", "http://go.dev/doc"},
		{"HTTPS://Go.dev/x?y=1#z", "https://Go.dev/x?y=1#z"},
		{"example.com", "https://example.com/"},
		{"example.com/path?q=1", "https://example.com/path?q=1"},
		{"sub.example.com:8443", "https://sub.example.com:8443/"},
		{"pão.com.br", "https://p%C3%A3o.com.br/"},
		{"localhost", "http://localhost/"},
		{"localhost:8080", "http://localhost:8080/"},
		{"app.localhost:3000/x", "http://app.localhost:3000/x"},
		{"printer.local", "http://printer.local/"},
		{"127.0.0.1:5000", "http://127.0.0.1:5000/"},
		{"192.168.0.10", "http://192.168.0.10/"},
		{"[::1]:8080", "http://[::1]:8080/"},
		{"8.8.8.8", "https://8.8.8.8/"},
		{"go os startprocess", "https://search.example/?q=go+os+startprocess"},
		{"golang", "https://search.example/?q=golang"},
		{"a&b=c#d", "https://search.example/?q=a%26b%3Dc%23d"},
		{"user@example.com", "https://search.example/?q=user%40example.com"},
		{"example..com", "https://search.example/?q=example..com"},
		{".com", "https://search.example/?q=.com"},
		{"  go.dev  ", "https://go.dev/"},
		{"docs https://go.dev", "https://search.example/?q=docs+https%3A%2F%2Fgo.dev"},
		{"compare http://a.com and https://b.com", "https://search.example/?q=compare+http%3A%2F%2Fa.com+and+https%3A%2F%2Fb.com"},
		{"what is ://", "https://search.example/?q=what+is+%3A%2F%2F"},
	}
	for _, c := range cases {
		got, err := resolve(c.in, s)
		if err != nil {
			t.Errorf("resolve(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("resolve(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A local page: a path, a name that is a file in the working directory, or
// a file: URL. The file has to exist and be a file.
func TestResolveLocalFile(t *testing.T) {
	dir := t.TempDir()
	page := filepath.Join(dir, "my page.html")
	err := os.WriteFile(page, []byte("<p>hi</p>"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = os.Mkdir(filepath.Join(dir, "sub"), 0o700)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	home, _ := os.UserHomeDir()
	want := "file://" + strings.ReplaceAll(page, " ", "%20")
	for _, in := range []string{
		page,
		"file://" + page,
		"file://localhost" + page,
		"FILE://" + strings.ReplaceAll(page, " ", "%20"),
		"./my page.html",
		"my page.html",
		"../" + filepath.Base(dir) + "/my page.html",
	} {
		got, err := resolve(in, defaultSearch)
		if err != nil || got != want {
			t.Errorf("resolve(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	got, err := resolve("~/", defaultSearch)
	if err == nil || !strings.Contains(err.Error(), home) {
		t.Errorf("resolve(~/) = %q, %v; want the home directory refused as not a file", got, err)
	}
	for _, in := range []string{
		"/no/such/file.html",
		"./missing.html",
		"./sub", // a directory
		".",
		"file:///no/such/file.html",
		"file://other.host" + page,
		"file://" + dir,
	} {
		got, err := resolve(in, defaultSearch)
		if err == nil {
			t.Errorf("resolve(%q) = %q, want error", in, got)
		}
	}
	// A name that is no file here is still a host or a search; a bare word
	// that is a directory is just a word.
	for in, want := range map[string]string{
		"missing.html": "https://missing.html/",
		"sub":          "https://duckduckgo.com/?q=sub",
	} {
		got, _ := resolve(in, defaultSearch)
		if got != want {
			t.Errorf("resolve(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveRejects(t *testing.T) {
	for _, in := range []string{
		"",
		"   ",
		"javascript://%0aalert(1)",
		"ftp://example.com",
		"https://",
		"https://exa mple.com",
		"https://go.dev docs",
	} {
		got, err := resolve(in, defaultSearch)
		if err == nil {
			t.Errorf("resolve(%q) = %q, want error", in, got)
		}
	}
}
