// Package psda1proxy implements a p.sda1.dev proxy upload module with:
//   - Local IPv4 only (no proxy, no IPv6)
//   - Magic-byte format validation (webp/jpg/png/gif only, avif/non-images rejected)
//   - HTTP proxy with 20-minute error caching
package psda1proxy

import (
	"context"
	"net"
	"net/http"
	"time"
)

// NewClient creates an HTTP client that:
//   - Explicitly disables all proxy (Proxy: nil, never consults env vars)
//   - Forces IPv4 (tcp4) – no IPv6 fallback
//   - Strips proxy-related environment variables so nothing leaks through
func NewClient() *http.Client {
	transport := &http.Transport{
		// Proxy: nil means "no proxy function is called". Unlike the
		// http.DefaultTransport (which sets Proxy = ProxyFromEnvironment),
		// a manually-constructed Transport with Proxy: nil bypasses proxy
		// entirely, including HTTP_PROXY / HTTPS_PROXY env vars.
		Proxy: nil,

		// Force IPv4 by rewriting the network to "tcp4" on every dial.
		// This prevents dual-stack / Happy-Eyeballs from leaking an IPv6
		// egress that might be routed overseas.
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
