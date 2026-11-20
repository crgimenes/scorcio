//go:build darwin || linux

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/crgimenes/filo"
)

type config struct {
	Search string
	// Browser is the command Open in Browser runs, the url appended as its
	// last argument; empty means the system's default browser.
	Browser []string
	// script is init.filo's engine, kept for the event hooks it defines;
	// nil without a file.
	script *filo.Filo
}

// No local ./scorcio_init.filo, unlike other tools: a browser starts from any
// directory, and a config dropped in a cloned repo could redirect searches.
func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "scorcio", "init.filo")
}

// loadConfig runs on defaults when the file does not exist. register, when
// set, adds the primitives the script may call before it runs.
func loadConfig(name string, register func(*filo.Filo) error) (config, error) {
	cfg := config{Search: defaultSearch}
	if name == "" {
		return cfg, nil
	}
	b, err := os.ReadFile(filepath.Clean(name))
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}

	f := filo.New()
	f.SetGlobal("Search", defaultSearch)
	f.SetGlobal("Browser", []string{})
	if register != nil {
		err = register(f)
		if err != nil {
			return cfg, err
		}
	}
	err = f.DoString(string(b))
	if isEmptyScript(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("%s: %w", name, err)
	}

	cfg.Search, err = f.GetString("Search")
	if err != nil {
		return cfg, fmt.Errorf("%s: Search: %w", name, err)
	}
	err = checkSearch(cfg.Search)
	if err != nil {
		return cfg, fmt.Errorf("%s: Search %q: %w", name, cfg.Search, err)
	}
	cfg.Browser, err = browserCommand(f)
	if err != nil {
		return cfg, fmt.Errorf("%s: Browser: %w", name, err)
	}
	cfg.script = f
	return cfg, nil
}

// browserCommand takes a list, argv without a shell, or a lone program name.
func browserCommand(f *filo.Filo) ([]string, error) {
	argv, err := f.GetTable("Browser")
	if err != nil {
		name, strErr := f.GetString("Browser")
		if strErr != nil {
			return nil, errors.New(`a list of strings, as (list "open" "-a" "Firefox")`)
		}
		argv = nil
		if name != "" {
			argv = []string{name}
		}
	}
	if len(argv) > 0 && argv[0] == "" {
		return nil, errors.New("the program is empty")
	}
	return argv, nil
}

// Filo refuses a script with no forms, but a config left all commented out
// just means "no settings".
func isEmptyScript(err error) bool {
	var pe *filo.ParseError
	return errors.As(err, &pe) && pe.Message == "empty script"
}

// Queries leave the machine, so plain http is only for a local provider.
func checkSearch(tmpl string) error {
	if strings.Count(tmpl, "%s") != 1 {
		return errors.New("needs exactly one %s where the query goes")
	}
	u, err := url.Parse(strings.Replace(tmpl, "%s", "q", 1))
	if err != nil {
		return err
	}
	if u.Host == "" {
		return errors.New("not an absolute URL")
	}
	host := u.Hostname()
	ip, ipErr := netip.ParseAddr(host)
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLocal(host, ip, ipErr == nil) {
			return nil
		}
		return errors.New("http is only allowed for a local search provider; use https")
	}
	return fmt.Errorf("unsupported scheme %q", u.Scheme)
}
