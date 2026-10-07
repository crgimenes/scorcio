//go:build darwin || linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crgimenes/glaze"
)

type zoomRecorder struct {
	glaze.WebView
	got []float64
}

func (r *zoomRecorder) SetZoom(f float64) { r.got = append(r.got, f) }

func TestZoomStepsStopAtTheEnds(t *testing.T) {
	if zoomSteps[actualSize] != 1 {
		t.Fatalf("zoomSteps[actualSize] = %v, want 1", zoomSteps[actualSize])
	}
	r := &zoomRecorder{}
	z := &zoomer{w: r, i: actualSize}
	z.step(1)
	z.step(-2)
	z.step(100)
	z.step(-100)
	z.step(actualSize - z.i)
	want := []float64{1.1, 0.9, 3, 0.5, 1}
	for i := range want {
		if i >= len(r.got) || r.got[i] != want[i] {
			t.Fatalf("zoom levels %v, want %v", r.got, want)
		}
	}
}

func TestInstanceArgs(t *testing.T) {
	orig := instanceFlags
	t.Cleanup(func() { instanceFlags = orig })
	cases := []struct {
		flags []string
		url   []string
		want  string
	}{
		{nil, nil, ""},
		{nil, []string{"-trace"}, "-- -trace"},
		{[]string{"-clean"}, nil, "-clean"},
		{[]string{"-clean"}, []string{"https://x.example/"}, "-clean -- https://x.example/"},
	}
	for _, c := range cases {
		instanceFlags = c.flags
		if got := strings.Join(instanceArgs(c.url...), " "); got != c.want {
			t.Errorf("flags %q url %q: %q, want %q", c.flags, c.url, got, c.want)
		}
	}
}

func TestOpenElsewhere(t *testing.T) {
	for _, page := range []string{"", "about:blank", "file:///etc/passwd", "javascript:alert(1)"} {
		err := openElsewhere([]string{"true"}, page)
		if err == nil {
			t.Errorf("openElsewhere(%q): want error", page)
		}
	}
	err := openElsewhere([]string{"true", "-x"}, "https://go.dev/")
	if err != nil {
		t.Errorf("openElsewhere with a command: %v", err)
	}
	err = openElsewhere([]string{"scorcio-no-such-program"}, "https://go.dev/")
	if err == nil {
		t.Error("openElsewhere with a missing program: want error")
	}
}

// Edit Config creates the file from the template, which loads to the same
// config as no file at all, and never touches a file that exists.
func TestEnsureConfig(t *testing.T) {
	name := filepath.Join(t.TempDir(), "scorcio", "init.filo")
	err := ensureConfig(name)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(name, nil)
	if err != nil {
		t.Fatalf("template does not load: %v", err)
	}
	if cfg.Search != defaultSearch || len(cfg.Browser) != 0 {
		t.Fatalf("template changed the config: %+v", cfg)
	}
	err = os.WriteFile(name, nil, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = ensureConfig(name)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(name) // #nosec G304 -- test temp file
	if err != nil || len(data) != 0 {
		t.Fatalf("existing file rewritten: %q, %v", data, err)
	}
}
