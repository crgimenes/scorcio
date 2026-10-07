//go:build darwin || linux

package main

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/crgimenes/glaze"
	"github.com/crgimenes/native/alert"
	"github.com/crgimenes/native/openurl"
)

// showDialog and openExternal are swapped out by tests.
var (
	showDialog   = alert.Show
	openExternal = openurl.Open
)

const stopDialogs = "Stop Dialogs"

// scriptDialog shows a page's alert, confirm or prompt under the name of the
// site asking, so a page cannot pass for the system. From the page's second
// dialog on, Stop Dialogs silences the rest until the next navigation: a page
// that asks in a loop would otherwise hold the window.
func (b *browser) scriptDialog(d glaze.ScriptDialog) (bool, string) {
	if b.dialogsStopped {
		return false, ""
	}
	opts := alert.Options{Title: b.site() + " says", Message: d.Message}
	switch d.Kind {
	case glaze.ScriptAlert:
		opts.Buttons = []alert.Button{{Title: "OK"}}
	case glaze.ScriptPrompt:
		opts.Input, opts.Text = true, d.Text
		fallthrough
	default:
		opts.Buttons = []alert.Button{{Title: "OK"}, {Title: "Cancel", Cancel: true}}
	}
	res, ok := b.dialog(opts)
	return ok && res.Button == 0, res.Text
}

// external offers a link to another app (mailto:, tel:, an app's own scheme)
// to the system, as the engine cannot show it; false for a scheme that is not
// for another app. file: stays out: handing a local path to the system can
// start a program.
func (b *browser) external(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "file", "javascript", "data", "blob", "about":
		return false
	}
	b.trace("event=external url=%q", raw)
	b.w.Dispatch(func() {
		res, ok := b.dialog(alert.Options{
			Title:   b.site() + " wants to open another app",
			Message: raw,
			Buttons: []alert.Button{{Title: "Open"}, {Title: "Cancel", Cancel: true}},
		})
		if !ok || res.Button != 0 {
			return
		}
		err := openExternal(raw)
		if err != nil {
			showError(fmt.Sprintf("%s: %v", raw, err))
		}
	})
	return true
}

// dialog shows opts with Stop Dialogs added from the page's second dialog on;
// ok is false when it was not shown, failed, or Stop Dialogs was chosen.
func (b *browser) dialog(opts alert.Options) (alert.Result, bool) {
	if b.dialogsStopped {
		return alert.Result{}, false
	}
	b.dialogs++
	stop := -1
	if b.dialogs > 1 {
		stop = len(opts.Buttons)
		opts.Buttons = append(opts.Buttons, alert.Button{Title: stopDialogs})
	}
	res, err := showDialog(opts)
	if err != nil {
		return alert.Result{}, false
	}
	if res.Button == stop {
		b.dialogsStopped = true
		return alert.Result{}, false
	}
	return res, true
}

// site names the page on screen for a dialog's title.
func (b *browser) site() string {
	u, err := url.Parse(b.current)
	if err != nil || u.Host == "" {
		return "This page"
	}
	return u.Host
}

func isWebURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
