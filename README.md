# scorcio

A small, fast, deliberately limited web browser: open a page, read it, leave.

> One page, one window, one process. Open the page, not the browser.

scorcio is not a replacement for Firefox, Chromium or Safari. It covers the
simplest frequent case -- get to a page quickly -- and hands everything else to
a full browser. It uses the web view the operating system already ships
(WKWebView on macOS, WebKitGTK on Linux) through
[glaze](https://github.com/crgimenes/glaze): no bundled engine, no cgo.

## Use

```sh
scorcio https://go.dev
scorcio go.dev                 # https:// is assumed
scorcio localhost:8080         # local addresses get http://
scorcio "go os startprocess"   # anything else goes to the search provider
scorcio "$(fzf < ~/.config/browser/bookmarks)"
bookmark-selector | scorcio    # first line of stdin
scorcio                        # asks for an address; Esc quits
```

| flag | |
|---|---|
| `-clean` | a clean session: nothing saved by earlier runs is read, nothing is kept; windows opened from it are clean too |
| `-trace` | timings on stderr: window, navigation start, url (at commit), shown (page first on screen), load (every resource in), failures |
| `-version`, `-h` | |

There are no tabs and no toolbar. A new window -- a `target=_blank` link, a
page's `window.open` after a click, a link another app opens with scorcio, or
New Window -- is a new scorcio process with a window of its own.

A page whose certificate is not valid does not load: the first page ends the
process with the error on stderr (and an alert when started from the Dock or
a launcher); later ones show an alert and the current page stays.

A page's alert, confirm and prompt show as dialogs titled with the site that
asks; from its second one on, Stop Dialogs silences the page until it loads a
new page. A link to another app (mailto:, tel:, an app's own scheme) is
offered to the system after asking; file: links never are.

Downloads always ask where to save. A page that wants the camera or microphone (a
call) gets them only after scorcio asks, once per site and process; location and
notifications are always refused.

## Keys

The macOS menus list them all; on Linux the same shortcuts use Ctrl.

| | |
|---|---|
| ⌘L | open location |
| ⌘O | open the page in a full browser |
| ⌘N | new window |
| ⌘W, ⌘Q | close |
| ⌘[ ⌘] | back, forward (or swipe two fingers on the trackpad) |
| ⌘R, ⌘. | reload, stop |
| ⌘F, ⌘G, ⇧⌘G | find, next, previous |
| ⌘= ⌘- ⌘0 | zoom in, out, actual size |
| ⌃⌘F | full screen (macOS) |
| ⌘, | edit the configuration (macOS) |

## Configuration

Optional: `init.filo` in `scorcio/` under the platform config directory
(`~/Library/Application Support` on macOS -- inside the app container when
sandboxed -- and `$XDG_CONFIG_HOME` on Linux), written in
[Filo](https://github.com/crgimenes/filo). On macOS, Edit Config (⌘,) opens it
in the default text editor, creating it with commented examples the first
time.

On macOS each window remembers its size and position, and a window opened
while others are up steps clear of them instead of covering them.

```lisp
(set Search "https://duckduckgo.com/?q=%s")

; Open in Browser (⌘O) runs this with the url appended, no shell;
; unset, the page goes to the system's default browser
(set Browser (list "open" "-a" "Firefox"))

; short names for the address box and the command line: "gh" opens
; GitHub, "w linux" searches Wikipedia
(alias "gh" "github.com")
(alias "w" "https://en.wikipedia.org/wiki/Special:Search?search=%s")

; styles a host and its subdomains ("*" is every site) as the page
; starts, so it never shows unstyled
(site-css "news.ycombinator.com" "body { font-size: 18px }")

; runs after each page loads
(def on-load (fn (url)
  (if (str-find "example.com/old" url)
    (navigate "example.com/new"))))
```

`on-load` may call `(navigate "url")`, `(reload)` and `(inject-css "css")`, and
the Filo string functions (`str-find`, `str-replace`, ...). Only the browser's
events reach the script; a page never can. Each call is bounded.

## Platforms and build

macOS and Linux (WebKitGTK 4.1 or 6.0 must be installed). Go, no cgo:

```sh
go build .
GOOS=linux GOARCH=arm64 go build .
```

On macOS, `release.sh` builds a signed, sandboxed `.app` from `assets/` (icon,
entitlements, Info.plist). scorcio opens no network port, not even on loopback.

On Linux, with `scorcio` in `PATH`, the desktop file puts it in the launcher and
in the list of browsers:

```sh
install -Dm644 assets/scorcio.desktop ~/.local/share/applications/scorcio.desktop
install -Dm644 assets/scorcio.png ~/.local/share/icons/hicolor/256x256/apps/scorcio.png
xdg-mime query default x-scheme-handler/https
```

scorcio never asks to be the default, but with no `mimeapps.list` naming one,
xdg-mime takes the first handler it finds, and the user's own applications come
first: if the query above answers `scorcio.desktop`, name your browser in
`~/.config/mimeapps.list`.
