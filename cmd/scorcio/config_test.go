//go:build darwin || linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t testing.TB, src string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "init.filo")
	err := os.WriteFile(name, []byte(src), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	return name
}

func TestLoadConfigDefaults(t *testing.T) {
	for _, name := range []string{"", filepath.Join(t.TempDir(), "missing.filo")} {
		cfg, err := loadConfig(name, nil)
		if err != nil {
			t.Fatalf("loadConfig(%q): %v", name, err)
		}
		if cfg.Search != defaultSearch {
			t.Errorf("loadConfig(%q).Search = %q, want default", name, cfg.Search)
		}
	}
}

func TestLoadConfigSearch(t *testing.T) {
	cases := []struct{ src, want string }{
		{`(set Search "https://www.google.com/search?q=%s")`, "https://www.google.com/search?q=%s"},
		{"\ufeff(set Search \"https://s.example/?q=%s\")\n", "https://s.example/?q=%s"},
		{`(set Search "http://127.0.0.1:8888/search?q=%s")`, "http://127.0.0.1:8888/search?q=%s"},
		{`(set Search "http://searx.local/?q=%s")`, "http://searx.local/?q=%s"},
		{"; nothing set\n", defaultSearch},
		{"", defaultSearch},
	}
	for _, c := range cases {
		cfg, err := loadConfig(writeConfig(t, c.src), nil)
		if err != nil {
			t.Errorf("%s: %v", c.src, err)
			continue
		}
		if cfg.Search != c.want {
			t.Errorf("%s: Search = %q, want %q", c.src, cfg.Search, c.want)
		}
	}
}

func TestLoadConfigRejects(t *testing.T) {
	for _, src := range []string{
		`(set Search "https://s.example/?q=")`,
		`(set Search "https://s.example/?q=%s&again=%s")`,
		`(set Search "http://s.example/?q=%s")`,
		`(set Search "ftp://s.example/%s")`,
		`(set Search "s.example/?q=%s")`,
		`(set Search 42)`,
		`(set Search "https://s.example/?q=%s"`,
		`(set Browser 1)`,
		`(set Browser (list "open" 2))`,
		`(set Browser (list "" "x"))`,
	} {
		_, err := loadConfig(writeConfig(t, src), nil)
		if err == nil {
			t.Errorf("%s: want error", src)
		}
	}
}

func TestLoadConfigBrowser(t *testing.T) {
	cases := []struct {
		src  string
		want []string
	}{
		{`(set Browser (list "open" "-a" "Google Chrome"))`, []string{"open", "-a", "Google Chrome"}},
		{`(set Browser "firefox")`, []string{"firefox"}},
		{`(set Browser "")`, nil},
		{`(set Browser (list))`, nil},
		{"; nothing set\n", nil},
	}
	for _, c := range cases {
		cfg, err := loadConfig(writeConfig(t, c.src), nil)
		if err != nil {
			t.Fatalf("%s: %v", c.src, err)
		}
		if strings.Join(cfg.Browser, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s: Browser = %q, want %q", c.src, cfg.Browser, c.want)
		}
	}
}
