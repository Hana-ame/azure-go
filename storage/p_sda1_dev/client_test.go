package p_sda1_dev

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNewHTTPClient_ReachesIPv4Server proves the client is functional against
// a plain IPv4 endpoint.
func TestNewHTTPClient_ReachesIPv4Server(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	resp, err := newHTTPClient().Get(ts.URL)
	if err != nil {
		t.Fatalf("expected success against IPv4 server, got: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
}

// TestNewHTTPClient_CannotReachIPv6OnlyServer is the regression test for the
// p.sda1.dev IPv6 403. It asserts the dialer is pinned to tcp4 by proving the
// client cannot reach an IPv6-only listener that a default client does reach.
func TestNewHTTPClient_CannotReachIPv6OnlyServer(t *testing.T) {
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("no IPv6 loopback available: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.Listener = ln
	srv.Start()
	defer srv.Close()

	resp, err := http.DefaultClient.Get(srv.URL)
	if err != nil {
		t.Fatalf("default client should reach the IPv6-only server, but it did not: %v", err)
	}
	resp.Body.Close()

	if _, err := newHTTPClient().Get(srv.URL); err == nil {
		t.Fatal("tcp4-pinned client unexpectedly reached an IPv6-only server")
	}
}

// TestNew_ConfirmsConfig asserts New() wires the upstream URLs and installs a
// client (not http.DefaultClient).
func TestNew_ConfirmsConfig(t *testing.T) {
	d := New()
	if d.baseURL != "https://p.sda1.dev" {
		t.Errorf("baseURL = %q, want https://p.sda1.dev", d.baseURL)
	}
	if d.apiURL != "https://p.sda1.dev/api/v1/upload_external_noform" {
		t.Errorf("apiURL = %q, want https://p.sda1.dev/api/v1/upload_external_noform", d.apiURL)
	}
	if d.client == nil {
		t.Fatal("client is nil")
	}
	if d.client == http.DefaultClient {
		t.Fatal("client is http.DefaultClient; it must be the tcp4-pinned client")
	}
}
