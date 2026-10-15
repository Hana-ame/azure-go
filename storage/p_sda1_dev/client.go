package p_sda1_dev

import (
	"context"
	"net"
	"net/http"
	"time"
)

// newHTTPClient returns the HTTP client used to talk to p.sda1.dev.
//
// It forces IPv4 (tcp4) and ignores proxy environment variables. Both choices
// are deliberate and established by measurement:
//
//  1. IPv4 only. p.sda1.dev's Cloudflare edge rejects IPv6 connections with
//     403 "error code: 1106". On a dual-homed host Go's default transport
//     sorts addresses per RFC 6724 and prefers the AAAA record, so
//     http.DefaultClient silently dials the IPv6 address and hits that block.
//     Captured on the deployed host with `ss -tnp`: p_sda1_dev.Driver dialed
//     2606:4700:3031::ac43:a64e (AAAA) -> 403, while psda1proxy.ProxyHandler
//     dialed 104.21.42.212 (A) -> 200, same process, same second.
//
//  2. Proxy: nil. Like psda1proxy.NewClient, this bypasses HTTP_PROXY /
//     HTTPS_PROXY so the behaviour is deterministic. Note the asymmetry this
//     creates: DefaultClient honoured env proxies, so a host that could only
//     reach p.sda1.dev via an overseas proxy would break after this change.
//     p.sda1.dev is behind Cloudflare and its SNI is on the GFW's SNI
//     blacklist, so a plain CN egress cannot reach it at all; any future CN
//     deployment needs an explicit proxy hook, not DefaultClient's
//     ProxyFromEnvironment.
//
// Import cycle: psda1proxy imports this package (psda1proxy/driver.go uses
// psda1.New()), so we cannot reuse psda1proxy.NewClient here. The transport
// below is an inline copy of that implementation (psda1proxy/client.go).
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
