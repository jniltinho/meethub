package httpserver

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// clientIP returns the client address: the TCP peer, unless the peer is a
// trusted proxy, in which case the right-most X-Forwarded-For entry that is not
// a trusted proxy. Headers from untrusted peers are ignored.
func clientIP(trusted []netip.Prefix) func(*http.Request) string {
	return func(r *http.Request) string {
		peer := remoteAddr(r)
		if !isTrusted(peer, trusted) {
			return peer.String()
		}
		hops := forwardedFor(r)
		for i := len(hops) - 1; i >= 0; i-- {
			if !isTrusted(hops[i], trusted) {
				return hops[i].String()
			}
		}
		if len(hops) > 0 { // every hop is a trusted proxy: the left-most is the origin
			return hops[0].String()
		}
		return peer.String()
	}
}

// requestScheme returns X-Forwarded-Proto only from trusted proxies.
func requestScheme(trusted []netip.Prefix) func(*http.Request) string {
	return func(r *http.Request) string {
		if isTrusted(remoteAddr(r), trusted) {
			if p := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))); p == "http" || p == "https" {
				return p
			}
		}
		if r.TLS != nil {
			return "https"
		}
		return "http"
	}
}

func remoteAddr(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, _ := netip.ParseAddr(host)
	return a.Unmap()
}

func forwardedFor(r *http.Request) []netip.Addr {
	var out []netip.Addr
	for _, h := range r.Header.Values("X-Forwarded-For") {
		for part := range strings.SplitSeq(h, ",") {
			if a, err := netip.ParseAddr(strings.TrimSpace(part)); err == nil {
				out = append(out, a.Unmap())
			}
		}
	}
	return out
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	if !a.IsValid() {
		return false
	}
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
