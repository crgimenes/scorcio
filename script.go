//go:build darwin || linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/crgimenes/filo"
	"github.com/crgimenes/filo/filostrings"
)

// init.filo may define event hooks and call the primitives below:
//
//	(site-css "news.ycombinator.com" "body { font-size: 18px }")
//	(def on-load (fn (url)
//	  (if (str-find "example.com" url) (navigate "example.org"))))
//
// Only the engine's events reach the script, with the URL as data; the page
// never does. Each call is bounded by Filo's integration limits.

const onLoad = "on-load"

func (b *browser) registerPrimitives(f *filo.Filo) error {
	filostrings.RegisterBuiltins(f.GetEngine())
	for name, fn := range map[string]filo.Builtin{
		"navigate":   b.primNavigate,
		"reload":     b.primReload,
		"inject-css": b.primInjectCSS,
		"site-css":   b.primSiteCSS,
		"alias":      b.primAlias,
	} {
		err := f.RegisterBuiltin(name, fn)
		if err != nil {
			return err
		}
	}
	return nil
}

var errNoPage = errors.New("no page yet: call it from an event hook, not while init.filo loads")

func oneString(name string, args []filo.Value) (string, error) {
	if len(args) != 1 || args[0].Kind != filo.KString {
		return "", fmt.Errorf("%s: want (%s %q)", name, name, "text")
	}
	return args[0].Str, nil
}

// primNavigate goes through the same checks as a typed address: only http
// and https load.
func (b *browser) primNavigate(_ context.Context, args []filo.Value) (filo.Value, error) {
	target, err := oneString("navigate", args)
	if err != nil {
		return filo.Value{}, err
	}
	if b.w == nil {
		return filo.Value{}, errNoPage
	}
	page, err := b.resolve(target)
	if err != nil {
		return filo.Value{}, fmt.Errorf("navigate: %w", err)
	}
	b.w.Navigate(page)
	return filo.VBool(true), nil
}

func (b *browser) primReload(_ context.Context, args []filo.Value) (filo.Value, error) {
	if len(args) != 0 {
		return filo.Value{}, errors.New("reload: takes no arguments")
	}
	if b.w == nil {
		return filo.Value{}, errNoPage
	}
	b.w.Reload()
	return filo.VBool(true), nil
}

// primInjectCSS styles the page on screen.
func (b *browser) primInjectCSS(_ context.Context, args []filo.Value) (filo.Value, error) {
	css, err := oneString("inject-css", args)
	if err != nil {
		return filo.Value{}, err
	}
	if b.w == nil {
		return filo.Value{}, errNoPage
	}
	lit, err := json.Marshal(css) // a JS string literal: the CSS stays data
	if err != nil {
		return filo.Value{}, err
	}
	b.w.Eval(`(function(c){` + applyCSS + `})(` + string(lit) + `)`)
	return filo.VBool(true), nil
}

// applyCSS adds the CSS in c to the document. A constructed sheet needs no
// element, so it works before <html> exists, is not an inline style for the
// page's Content-Security-Policy to refuse, and comes after the page's own
// sheets in the cascade. The style element is the fallback for an engine
// without constructed sheets.
const applyCSS = `try{var s=new CSSStyleSheet();s.replaceSync(c);` +
	`document.adoptedStyleSheets=document.adoptedStyleSheets.concat([s]);return}catch(e){}` +
	`var el=document.createElement("style");el.textContent=c;` +
	`(document.head||document.documentElement).appendChild(el)`

// siteStyle is CSS for a host and its subdomains; "*" is every site.
type siteStyle struct{ Host, CSS string }

// primSiteCSS collects CSS that goes in before a matching page first paints.
// It only runs while init.filo loads: the rules become one script the engine
// runs at the start of every document.
func (b *browser) primSiteCSS(_ context.Context, args []filo.Value) (filo.Value, error) {
	if len(args) != 2 || args[0].Kind != filo.KString || args[1].Kind != filo.KString {
		return filo.Value{}, errors.New(`site-css: want (site-css "host" "css")`)
	}
	if b.w != nil {
		return filo.Value{}, errors.New("site-css: only while init.filo loads; in a hook, use inject-css")
	}
	host := strings.ToLower(strings.TrimSpace(args[0].Str))
	if host == "" || strings.ContainsAny(host, "/:@ \t") {
		return filo.Value{}, fmt.Errorf("site-css: %q: want a host name, as \"example.com\", or \"*\"", args[0].Str)
	}
	b.styles = append(b.styles, siteStyle{Host: strings.TrimPrefix(host, "."), CSS: args[1].Str})
	return filo.VBool(true), nil
}

// primAlias names an address, or with %s a search, for the address box and
// the command line. Only while init.filo loads, like site-css.
func (b *browser) primAlias(_ context.Context, args []filo.Value) (filo.Value, error) {
	if len(args) != 2 || args[0].Kind != filo.KString || args[1].Kind != filo.KString {
		return filo.Value{}, errors.New(`alias: want (alias "name" "address")`)
	}
	if b.w != nil {
		return filo.Value{}, errors.New("alias: only while init.filo loads")
	}
	name, target := args[0].Str, strings.TrimSpace(args[1].Str)
	if name == "" || strings.ContainsAny(name, " \t") {
		return filo.Value{}, fmt.Errorf("alias: %q: want one word", name)
	}
	err := checkAliasTarget(target)
	if err != nil {
		return filo.Value{}, fmt.Errorf("alias %s: %q: %w", name, target, err)
	}
	if b.aliases == nil {
		b.aliases = map[string]string{}
	}
	b.aliases[name] = target
	return filo.VBool(true), nil
}

// checkAliasTarget holds a search target to the provider's rules and an
// address to resolve's, so an alias never reaches what typing could not.
func checkAliasTarget(target string) error {
	if strings.Contains(target, "%s") {
		return checkSearch(target)
	}
	if hasScheme(target) {
		_, err := explicitURL(target)
		return err
	}
	_, ok := bareAddress(target)
	if !ok {
		return errors.New("not an address")
	}
	return nil
}

// initStyles has the engine run the site-css rules at the start of every
// document.
func (b *browser) initStyles() {
	js := styleScript(b.styles)
	if js != "" {
		b.w.Init(js)
	}
}

// styleScript is the document-start script for the rules; "" without any.
func styleScript(styles []siteStyle) string {
	if len(styles) == 0 {
		return ""
	}
	rules, _ := json.Marshal(styles) // only strings: cannot fail
	return `(function(r){var h=location.hostname,c="";` +
		`r.forEach(function(x){if(x.Host==="*"||h===x.Host||h.endsWith("."+x.Host))c+=x.CSS+"\n"});` +
		`if(!c)return;(function(c){` + applyCSS + `})(c)})(` + string(rules) + `)`
}

// loaded runs init.filo's on-load hook, if there is one.
func (b *browser) loaded(url string) {
	if b.script == nil || !b.script.HasFunction(onLoad) {
		return
	}
	start := time.Now()
	_, err := b.script.CallFunction(onLoad, url)
	b.trace("event=filo fn=%s ms=%.2f", onLoad, float64(time.Since(start).Microseconds())/1000)
	if err != nil {
		fmt.Fprintln(os.Stderr, "scorcio: init.filo:", err)
	}
}
