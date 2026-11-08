package p_inari_site

import (
	"context"
	"net"
	"net/http"
	"time"
)

// newHTTPClient returns the HTTP client used to talk to up.inari.site for
// uploads (session acquisition + POST /upload).
//
// Like p_sda1_dev, it forces IPv4 (tcp4) and ignores proxy environment
// variables. Both choices are deliberate and established by measurement on
// p.sda1.dev:
//
//  1. IPv4 only. up.inari.site sits behind the same Cloudflare edge (server:
//     cloudflare, cf-ray header present) and Cloudflare rejects IPv6
//     connections with 403 "error code: 1106" on some egress networks. Pinning
//     tcp4 avoids RFC 6724 address sorting ever dialing the AAAA record first.
//
//  2. Proxy: nil. Bypasses HTTP_PROXY / HTTPS_PROXY so the behaviour is
//     deterministic; this host must reach up.inari.site directly. The session
//     (cookie + CSRF token) comes from the same origin, so going direct keeps
//     the credential pair consistent.
//
// This is an inline copy of p_sda1_dev.newHTTPClient (psda1proxy already
// imports p_sda1_dev, so reusing that client would risk an import cycle).
// Keep the two in sync if either changes.
func newHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: nil,

		// Rewrite the network to "tcp4" on every dial so dual-stack /
		// Happy-Eyeballs can never leak an IPv6 egress.
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext(ctx, "tcp4", addr)
		},

		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   120 * time.Second,
	}
}

// newReadClient returns the client used to fetch public objects back from
// p.inari.site. Unlike the upload client this one honours HTTP_PROXY /
// HTTPS_PROXY (ProxyFromEnvironment): the read host may sit behind a proxy on
// some egress networks, and the production host simply has no proxy env set so
// it degrades to direct. It stays IPv4-pinned for the same Cloudflare reason.
func newReadClient() *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,

		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext(ctx, "tcp4", addr)
		},

		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   120 * time.Second,
	}
}
