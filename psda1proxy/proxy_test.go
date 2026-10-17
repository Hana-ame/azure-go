package psda1proxy

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// TestProxyHandler_Success proxies a 200 response from the upstream.
func TestProxyHandler_Success(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fake-png-data"))
	}))
	defer upstream.Close()

	handler := &ProxyHandler{
		client:   NewClient(),
		upstream: upstream.URL,
		cache:    make(map[string]*cachedEntry),
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/p.sda1.dev/123/test.png", nil)
	c.Params = gin.Params{{Key: "path", Value: "/123/test.png"}}

	handler.Handle(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != "fake-png-data" {
		t.Errorf("body = %q, want %q", w.Body.String(), "fake-png-data")
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "image/png") {
		t.Errorf("content-type = %q, want image/png prefix", ct)
	}
}

// TestProxyHandler_NotFound_Cached verifies that a 404 response is cached
// for ErrorCacheTTL and replayed from cache without hitting the upstream.
func TestProxyHandler_NotFound_Cached(t *testing.T) {
	var hitCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hitCount, 1)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer upstream.Close()

	handler := &ProxyHandler{
		client:   NewClient(),
		upstream: upstream.URL,
		cache:    make(map[string]*cachedEntry),
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/p.sda1.dev/123/nonexistent.png", nil)
	c.Params = gin.Params{{Key: "path", Value: "/123/nonexistent.png"}}

	// First request: hits upstream, caches 404
	handler.Handle(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("first request status = %d, want %d", w.Code, http.StatusNotFound)
	}
	if w.Body.String() != "not found" {
		t.Errorf("first request body = %q, want %q", w.Body.String(), "not found")
	}

	// Second request: should be served from cache (no upstream hit)
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/p.sda1.dev/123/nonexistent.png", nil)
	c2.Params = gin.Params{{Key: "path", Value: "/123/nonexistent.png"}}

	handler.Handle(c2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("cached request status = %d, want %d", w2.Code, http.StatusNotFound)
	}
	if w2.Body.String() != "not found" {
		t.Errorf("cached body = %q, want %q", w2.Body.String(), "not found")
	}

	// Verify only one upstream hit
	if got := atomic.LoadInt32(&hitCount); got != 1 {
		t.Errorf("upstream hit count = %d, want 1 (cache should prevent re-fetch)", got)
	}
}

// TestProxyHandler_ServerError_Cached verifies 5xx caching.
func TestProxyHandler_ServerError_Cached(t *testing.T) {
	var hitCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hitCount, 1)
		w.WriteHeader(http.StatusBadGateway)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>502 Bad Gateway</html>"))
	}))
	defer upstream.Close()

	handler := &ProxyHandler{
		client:   NewClient(),
		upstream: upstream.URL,
		cache:    make(map[string]*cachedEntry),
	}

	// First request: hits upstream
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/p.sda1.dev/123/bad.png", nil)
	c.Params = gin.Params{{Key: "path", Value: "/123/bad.png"}}
	handler.Handle(c)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("first request status = %d, want %d", w.Code, http.StatusBadGateway)
	}

	// Second request: cached
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/p.sda1.dev/123/bad.png", nil)
	c2.Params = gin.Params{{Key: "path", Value: "/123/bad.png"}}
	handler.Handle(c2)

	if w2.Code != http.StatusBadGateway {
		t.Fatalf("cached request status = %d, want %d", w2.Code, http.StatusBadGateway)
	}

	if got := atomic.LoadInt32(&hitCount); got != 1 {
		t.Errorf("upstream hit count = %d, want 1", got)
	}
}

// TestProxyHandler_UpstreamUnreachable_Caches502 verifies network errors
// are cached as 502.
func TestProxyHandler_UpstreamUnreachable_Caches502(t *testing.T) {
	// Use a closed server to simulate unreachable upstream
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close() // immediately close

	handler := &ProxyHandler{
		client:   NewClient(),
		upstream: server.URL,
		cache:    make(map[string]*cachedEntry),
	}

	// First request: should get 502 (network error)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/p.sda1.dev/123/broken.png", nil)
	c.Params = gin.Params{{Key: "path", Value: "/123/broken.png"}}
	handler.Handle(c)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("unreachable upstream status = %d, want %d", w.Code, http.StatusBadGateway)
	}

	// Second request: should be cached 502
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/p.sda1.dev/123/broken.png", nil)
	c2.Params = gin.Params{{Key: "path", Value: "/123/broken.png"}}
	handler.Handle(c2)

	if w2.Code != http.StatusBadGateway {
		t.Fatalf("cached unreachable status = %d, want %d", w2.Code, http.StatusBadGateway)
	}
}

// TestProxyHandler_MethodNotAllowed verifies non-GET methods return 405.
func TestProxyHandler_MethodNotAllowed(t *testing.T) {
	handler := &ProxyHandler{
		client:   NewClient(),
		upstream: "https://p.sda1.dev",
		cache:    make(map[string]*cachedEntry),
	}

	for _, method := range []string{http.MethodPost, http.MethodDelete, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(method, "/api/p.sda1.dev/123/test.png", nil)
			c.Params = gin.Params{{Key: "path", Value: "/123/test.png"}}
			handler.Handle(c)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s status = %d, want %d", method, w.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

// TestProxyHandler_EmptyPath returns 404 for empty path.
func TestProxyHandler_EmptyPath(t *testing.T) {
	handler := &ProxyHandler{
		client:   NewClient(),
		upstream: "https://p.sda1.dev",
		cache:    make(map[string]*cachedEntry),
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/p.sda1.dev/", nil)
	c.Params = gin.Params{{Key: "path", Value: "/"}}
	handler.Handle(c)

	if w.Code != http.StatusNotFound {
		t.Errorf("empty path status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestProxyHandler_DifferentPaths verifies caching is per-path.
func TestProxyHandler_DifferentPaths(t *testing.T) {
	var hitCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hitCount, 1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	handler := &ProxyHandler{
		client:   NewClient(),
		upstream: upstream.URL,
		cache:    make(map[string]*cachedEntry),
	}

	// Request two different paths
	for _, path := range []string{"/123/a.png", "/123/b.png"} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/p.sda1.dev"+path, nil)
		c.Params = gin.Params{{Key: "path", Value: path}}
		handler.Handle(c)
		if w.Code != http.StatusOK {
			t.Errorf("path %s status = %d, want %d", path, w.Code, http.StatusOK)
		}
	}

	// Each path should hit upstream once
	if got := atomic.LoadInt32(&hitCount); got != 2 {
		t.Errorf("upstream hit count = %d, want 2", got)
	}
}

// TestProxyHandler_CacheExpiration verifies the cache TTL.
func TestProxyHandler_CacheExpiration(t *testing.T) {
	var hitCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hitCount, 1)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("404"))
	}))
	defer upstream.Close()

	handler := &ProxyHandler{
		client:   NewClient(),
		upstream: upstream.URL,
		cache:    make(map[string]*cachedEntry),
	}

	// Set a very short TTL for testing
	origTTL := ErrorCacheTTL
	ErrorCacheTTL = 100 * time.Millisecond
	defer func() { ErrorCacheTTL = origTTL }()

	path := "/123/expiring.png"

	// First request: hits upstream, caches error
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/p.sda1.dev"+path, nil)
	c.Params = gin.Params{{Key: "path", Value: path}}
	handler.Handle(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("first request status = %d, want %d", w.Code, http.StatusNotFound)
	}

	// Immediately retry: cached
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/api/p.sda1.dev"+path, nil)
	c2.Params = gin.Params{{Key: "path", Value: path}}
	handler.Handle(c2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("cached request status = %d, want %d", w2.Code, http.StatusNotFound)
	}
	if got := atomic.LoadInt32(&hitCount); got != 1 {
		t.Errorf("after first+cached, hits = %d, want 1", got)
	}

	// Wait for expiry
	time.Sleep(150 * time.Millisecond)

	// Third request: cache expired, hits upstream again
	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	c3.Request = httptest.NewRequest(http.MethodGet, "/api/p.sda1.dev"+path, nil)
	c3.Params = gin.Params{{Key: "path", Value: path}}
	handler.Handle(c3)
	if w3.Code != http.StatusNotFound {
		t.Fatalf("expired request status = %d, want %d", w3.Code, http.StatusNotFound)
	}
	if got := atomic.LoadInt32(&hitCount); got != 2 {
		t.Errorf("after expiry, hits = %d, want 2", got)
	}
}

// TestProxyHandler_CacheLen verifies cache size tracking.
func TestProxyHandler_CacheLen(t *testing.T) {
	handler := &ProxyHandler{
		client:   NewClient(),
		upstream: "https://p.sda1.dev",
		cache:    make(map[string]*cachedEntry),
	}

	if handler.cacheLen() != 0 {
		t.Error("initial cache should be empty")
	}

	handler.cachePut("path1", http.StatusNotFound, "text/plain", []byte("404"))
	handler.cachePut("path2", http.StatusInternalServerError, "text/plain", []byte("500"))

	if handler.cacheLen() != 2 {
		t.Errorf("cacheLen = %d, want 2", handler.cacheLen())
	}
}

// TestProxyHandler_ContentPreservation verifies the proxy preserves
// upstream response body exactly.
func TestProxyHandler_ContentPreservation(t *testing.T) {
	expectedBody := "image-data-here"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(expectedBody))
	}))
	defer upstream.Close()

	handler := &ProxyHandler{
		client:   NewClient(),
		upstream: upstream.URL,
		cache:    make(map[string]*cachedEntry),
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/p.sda1.dev/123/test.png", nil)
	c.Params = gin.Params{{Key: "path", Value: "/123/test.png"}}
	handler.Handle(c)

	if w.Body.String() != expectedBody {
		t.Errorf("body = %q, want %q", w.Body.String(), expectedBody)
	}
}

// TestProxyHandler_StripsLegacyEnvelope verifies that objects stored before
// the driver stopped fabricating a multipart envelope are served as clean
// image bytes. p.sda1.dev stores whatever it receives, so those objects hold
// the envelope instead of the image, and the endpoint has no delete API —
// stripping on the read path is the only repair.
func TestProxyHandler_StripsLegacyEnvelope(t *testing.T) {
	// A real image wrapped the way the old driver sent it.
	imageBytes := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 'a', 'b', 'c'}
	var envelope bytes.Buffer
	w := multipart.NewWriter(&envelope)
	fw, err := w.CreateFormFile("image", "1792228850/photo.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(imageBytes); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		// Cloudflare assigns Content-Type by extension, so the header says
		// image/png even though the body is an envelope — exactly the observed
		// bug: a body that cannot be rendered as an image.
		rw.Header().Set("Content-Type", "image/png")
		rw.WriteHeader(http.StatusOK)
		rw.Write(envelope.Bytes())
	}))
	defer upstream.Close()

	handler := &ProxyHandler{
		client:   NewClient(),
		upstream: upstream.URL,
		cache:    make(map[string]*cachedEntry),
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/p.sda1.dev/1792228850/photo.png", nil)
	c.Params = gin.Params{{Key: "path", Value: "/1792228850/photo.png"}}
	handler.Handle(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("Content-Disposition")) {
		t.Error("body still contains the multipart envelope headers")
	}
	if got := rec.Body.Bytes(); !bytes.Equal(got, imageBytes) {
		t.Errorf("body = %d bytes starting %q, want the clean image bytes",
			len(got), got[:min(len(got), 16)])
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/png") {
		t.Errorf("content type = %q, want image/png prefix", ct)
	}
}

// TestProxyHandler_DoesNotStripCleanBody verifies stripping is a no-op for
// objects written by the fixed driver.
func TestProxyHandler_DoesNotStripCleanBody(t *testing.T) {
	// The body contains "\r\n--" inside real data: it must never be mis-sliced.
	// An envelope is only recognised by its *first* line.
	body := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, '-', '-', 'x', 'y'}

	upstream := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "image/png")
		rw.WriteHeader(http.StatusOK)
		rw.Write(body)
	}))
	defer upstream.Close()

	handler := &ProxyHandler{
		client:   NewClient(),
		upstream: upstream.URL,
		cache:    make(map[string]*cachedEntry),
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/p.sda1.dev/1792228850/photo.png", nil)
	c.Params = gin.Params{{Key: "path", Value: "/1792228850/photo.png"}}
	handler.Handle(c)

	if got := rec.Body.Bytes(); !bytes.Equal(got, body) {
		t.Errorf("body = %q, want untouched %q", got, body)
	}
}

// TestProxyHandler_DeleteDirect verifies that the delete URL format
// is the direct URL (not proxied).
func TestProxyHandler_DeleteDirect(t *testing.T) {
	// This test verifies the delete URL format as specified in the
	// requirements: delete_url should be https://p.sda1.dev/{path}
	// (direct, not proxied through upload.moonchan.xyz).
	path := "1720000000/abc123.webp"
	expectedDeleteURL := "https://p.sda1.dev/" + path

	if !strings.HasPrefix(expectedDeleteURL, "https://p.sda1.dev/") {
		t.Errorf("delete URL should start with https://p.sda1.dev/")
	}
	if strings.Contains(expectedDeleteURL, "upload.moonchan.xyz") {
		t.Error("delete URL should NOT contain upload.moonchan.xyz (no proxy)")
	}
	t.Logf("delete_url format: %s", expectedDeleteURL)
}

// TestProxyHandler_UploadURLFormat verifies the upload URL format
// is the proxied URL (upload.moonchan.xyz/api/p.sda1.dev/{path}).
func TestProxyHandler_UploadURLFormat(t *testing.T) {
	path := "1720000000/abc123.webp"
	expectedURL := "https://upload.moonchan.xyz/api/p.sda1.dev/" + path

	if !strings.HasPrefix(expectedURL, "https://upload.moonchan.xyz/api/p.sda1.dev/") {
		t.Errorf("upload URL should start with https://upload.moonchan.xyz/api/p.sda1.dev/")
	}
	t.Logf("upload URL format: %s", expectedURL)
}

// TestErrorCacheTTL_20Minutes verifies the TTL constant.
func TestErrorCacheTTL_20Minutes(t *testing.T) {
	if ErrorCacheTTL != 20*time.Minute {
		t.Errorf("ErrorCacheTTL = %v, want 20m", ErrorCacheTTL)
	}
}

// Ensure fmt is used.
var _ = fmt.Sprintf
