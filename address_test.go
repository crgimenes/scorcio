//go:build darwin || linux

package main

import "testing"

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

func TestResolveRejects(t *testing.T) {
	for _, in := range []string{
		"",
		"   ",
		"file:///etc/passwd",
		"javascript://%0aalert(1)",
		"ftp://example.com",
		"https://",
		"https://exa mple.com",
		"https://go.dev docs",
		"FILE:///etc/passwd",
	} {
		got, err := resolve(in, defaultSearch)
		if err == nil {
			t.Errorf("resolve(%q) = %q, want error", in, got)
		}
	}
}
