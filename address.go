//go:build darwin || linux

package main

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"unicode"
)

const defaultSearch = "https://duckduckgo.com/?q=%s"

// resolve turns what the user typed into the URL to load. Anything that does
// not read as an address goes to the search provider; only http and https
// are ever loaded.
func resolve(input, search string) (string, error) {
	in := strings.TrimSpace(input)
	if in == "" {
		return "", errors.New("empty address")
	}
	if hasScheme(in) {
		return explicitURL(in)
	}
	u, ok := bareAddress(in)
	if ok {
		return u, nil
	}
	return strings.Replace(search, "%s", url.QueryEscape(in), 1), nil
}

// expandAlias rewrites input whose first word is an alias from init.filo. A
// target with %s takes the rest of the input as a query; one without it
// stands for the alias alone.
func expandAlias(input string, aliases map[string]string) string {
	in := strings.TrimSpace(input)
	word, rest, _ := strings.Cut(in, " ")
	target, ok := aliases[word]
	if !ok {
		return input
	}
	rest = strings.TrimSpace(rest)
	if strings.Contains(target, "%s") {
		return strings.Replace(target, "%s", url.QueryEscape(rest), 1)
	}
	if rest != "" {
		return input
	}
	return target
}

// hasScheme reports input that starts with "scheme://" (RFC 3986 scheme
// characters); a URL later in the text, as in a query that quotes a link, does
// not make the input an address.
func hasScheme(in string) bool {
	scheme, _, found := strings.Cut(in, "://")
	if !found || scheme == "" || !isASCIILetter(rune(scheme[0])) {
		return false
	}
	for _, r := range scheme {
		switch {
		case isASCIILetter(r), '0' <= r && r <= '9', r == '+', r == '-', r == '.':
		default:
			return false
		}
	}
	return true
}

func isASCIILetter(r rune) bool { return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' }

func explicitURL(in string) (string, error) {
	u, err := url.Parse(in)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("missing host")
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}

// bareAddress accepts host[:port][/path] with no scheme. A host needs a dot,
// or to be localhost or an IP literal; a single word is a search. Userinfo is
// refused: "user@host" typed without a scheme is far likelier an e-mail or a
// lure than a login.
func bareAddress(in string) (string, bool) {
	if strings.ContainsAny(in, " \t@") {
		return "", false
	}
	u, err := url.Parse("//" + in)
	if err != nil || u.Host == "" {
		return "", false
	}
	host := u.Hostname()
	ip, ipErr := netip.ParseAddr(host)
	isIP := ipErr == nil
	if !isIP && !strings.Contains(host, ".") && host != "localhost" {
		return "", false
	}
	if !isIP && !validHostname(host) {
		return "", false
	}
	u.Scheme = "https"
	if isLocal(host, ip, isIP) {
		u.Scheme = "http"
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), true
}

func validHostname(host string) bool {
	if strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.Contains(host, "..") {
		return false
	}
	for _, r := range host {
		if r != '-' && r != '.' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// Local services rarely have a certificate, and a failed TLS handshake ends
// the process, so they get plain http.
func isLocal(host string, ip netip.Addr, isIP bool) bool {
	if isIP {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	h := strings.ToLower(host)
	return h == "localhost" ||
		strings.HasSuffix(h, ".localhost") ||
		strings.HasSuffix(h, ".local")
}
