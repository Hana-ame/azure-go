package psda1proxy

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ErrorCacheTTL is how long a non-200 upstream response is cached.
// During this window the same path is served from cache without
// re-hitting p.sda1.dev, protecting the upstream from error storms.
// (Declared as var, not const, so tests can shorten it.)
var ErrorCacheTTL = 20 * time.Minute

// cachedEntry stores a non-200 upstream response for later replay.
type cachedEntry struct {
	statusCode  int
	contentType string
	body        []byte
	expiresAt   time.Time
}

// ProxyHandler serves GET /api/p.sda1.dev/*path by proxying to
// https://p.sda1.dev/*path. Non-200 responses are cached for
// ErrorCacheTTL (20 minutes) and replayed from memory.
type ProxyHandler struct {
	client   *http.Client
	upstream string // e.g. "https://p.sda1.dev"

	mu    sync.RWMutex
	cache map[string]*cachedEntry
}

// NewProxyHandler creates a proxy handler that uses a no-proxy,
// IPv4-only HTTP client for upstream requests.
func NewProxyHandler() *ProxyHandler {
	h := &ProxyHandler{
		client:   NewClient(),
		upstream: "https://p.sda1.dev",
		cache:    make(map[string]*cachedEntry),
	}
	// Background goroutine purges expired entries every 5 minutes.
	go h.cleanupLoop()
	return h
}

// Handle is the gin handler for GET /api/p.sda1.dev/*path.
//
// Flow:
//  1. Extract path from the URL
//  2. If path is in the error cache → return cached error immediately
//  3. Proxy GET to https://p.sda1.dev/{path}
//  4. If upstream returns 200 → stream content to the client
//  5. If upstream returns non-200 → cache error for 20 min, return it
//  6. If upstream is unreachable → cache 502 Bad Gateway for 20 min
func (h *ProxyHandler) Handle(c *gin.Context) {
	// Only GET is proxied; other methods get 405 (delete goes direct).
	if c.Request.Method != http.MethodGet {
		c.String(http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}

	path := strings.TrimPrefix(c.Param("path"), "/")
	if path == "" {
		c.String(http.StatusNotFound, "Not Found")
		return
	}

	// Step 1: check cache
	if entry := h.cacheGet(path); entry != nil {
		c.Data(entry.statusCode, entry.contentType, entry.body)
		return
	}

	// Step 2: proxy upstream
	upstreamURL := h.upstream + "/" + path
	resp, err := h.client.Get(upstreamURL)
	if err != nil {
		// Network error → cache 502 for 20 min
		h.cachePut(path, http.StatusBadGateway, "text/plain; charset=utf-8",
			[]byte("Bad Gateway: upstream unreachable"))
		c.String(http.StatusBadGateway, "Bad Gateway: upstream unreachable")
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		h.cachePut(path, http.StatusBadGateway, "text/plain; charset=utf-8",
			[]byte("Bad Gateway: upstream read error"))
		c.String(http.StatusBadGateway, "Bad Gateway: upstream read error")
		return
	}

	// Step 3: non-200 → cache the error
	if resp.StatusCode != http.StatusOK {
		ct := resp.Header.Get("Content-Type")
		if ct == "" {
			ct = "text/plain; charset=utf-8"
		}
		h.cachePut(path, resp.StatusCode, ct, body)
		c.Data(resp.StatusCode, ct, body)
		return
	}

	// Step 4: 200 → return content
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}
	c.Data(http.StatusOK, ct, body)
}

// --- cache helpers ---

func (h *ProxyHandler) cacheGet(path string) *cachedEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()
	e, ok := h.cache[path]
	if !ok {
		return nil
	}
	if time.Now().After(e.expiresAt) {
		// Expired – do NOT return it; the caller will re-fetch.
		return nil
	}
	return e
}

func (h *ProxyHandler) cachePut(path string, statusCode int, contentType string, body []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cache[path] = &cachedEntry{
		statusCode:  statusCode,
		contentType: contentType,
		body:        body,
		expiresAt:   time.Now().Add(ErrorCacheTTL),
	}
}

// cacheLen returns the number of cached entries (for diagnostics/tests).
func (h *ProxyHandler) cacheLen() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.cache)
}

// cleanupLoop periodically purges expired cache entries.
func (h *ProxyHandler) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		h.mu.Lock()
		now := time.Now()
		for path, e := range h.cache {
			if now.After(e.expiresAt) {
				delete(h.cache, path)
			}
		}
		h.mu.Unlock()
	}
}
