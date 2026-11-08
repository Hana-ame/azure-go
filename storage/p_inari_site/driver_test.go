package p_inari_site

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testHomepage = `<!DOCTYPE html><html><head>
<meta name="csrf-token" content="%s">
</head><body>PigeonNet Pics Hosting</body></html>`

// testPNG is a tiny valid PNG signature (contents irrelevant to the driver).
var testPNG = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x01, 0x02, 0x03, 0x04}

// fakeUpstream simulates Lsky Pro: the homepage issues a session cookie and a
// CSRF token; /upload requires both to match, else 419.
type fakeUpstream struct {
	mu           sync.Mutex
	homeHits     int
	uploadHits   int
	lastToken    string
	lastCookie   string
	lastFilename string
	// failUploadWith419 forces the next /upload to return 419 once.
	failUploadWith419 int
}

func newFakeUpstream() *fakeUpstream {
	return &fakeUpstream{}
}

func (f *fakeUpstream) sessionCookie() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return "lsky_pro_session=session-" + time.Now().Format("150405.000000000")
}

func (f *fakeUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/" && r.Method == http.MethodGet:
		f.mu.Lock()
		f.homeHits++
		f.mu.Unlock()
		token := "tok-" + r.URL.Query().Get("n")
		if token == "tok-" {
			token = "tok-1"
		}
		http.SetCookie(w, &http.Cookie{Name: "lsky_pro_session", Value: "session-" + token, Path: "/"})
		w.Write([]byte(strings.Replace(testHomepage, "%s", token, 1)))

	case r.URL.Path == "/upload" && r.Method == http.MethodPost:
		f.mu.Lock()
		f.uploadHits++
		force419 := f.failUploadWith419 > 0
		if force419 {
			f.failUploadWith419--
		}
		f.mu.Unlock()

		cookie := ""
		if c, err := r.Cookie("lsky_pro_session"); err == nil {
			cookie = c.Value
		}
		token := r.Header.Get("X-CSRF-Token")

		// Parse the multipart file part.
		var filename string
		var fileBytes []byte
		if mr, err := r.MultipartReader(); err == nil {
			for {
				part, err := mr.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					break
				}
				if part.FormName() == "file" {
					filename = part.FileName()
					fileBytes, _ = io.ReadAll(part)
				}
				part.Close()
			}
		}

		f.mu.Lock()
		f.lastToken = token
		f.lastCookie = cookie
		f.lastFilename = filename
		f.mu.Unlock()

		if force419 || cookie == "" || !strings.HasPrefix(cookie, "session-"+token) {
			w.WriteHeader(httpStatusExpired)
			w.Write([]byte(`{"status":false,"message":"419 Page Expired"}`))
			return
		}
		if !bytes.HasPrefix(fileBytes, testPNG[:8]) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"status":false,"message":"not an image"}`))
			return
		}

		pathname := "26-11/08/abc123.png"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":  true,
			"message": "上传成功",
			"data": map[string]any{
				"pathname": pathname,
				"md5":      "28f812512e9f334073eadbbad028dd3a",
				"links": map[string]any{
					"url": "https://p.inari.site/guest/" + pathname,
				},
			},
		})

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// newTestDriver wires a Driver to the fake upstream. The session client keeps
// the driver's own cookie jar (a ts.Client() has no jar, so the cookie would
// never be stored and every upload would 419). Its tcp4 transport reaches the
// httptest server on 127.0.0.1 fine.
func newTestDriver(ts *httptest.Server) *Driver {
	d := New()
	d.baseURL = ts.URL
	d.uploadURL = ts.URL + "/upload"
	d.publicBaseURL = "https://p.inari.site"
	d.read = ts.Client()
	d.sessionTTL = time.Hour
	return d
}

func TestNew_ConfirmsConfig(t *testing.T) {
	d := New()
	if d.baseURL != "https://up.inari.site" {
		t.Errorf("baseURL = %q", d.baseURL)
	}
	if d.uploadURL != "https://up.inari.site/upload" {
		t.Errorf("uploadURL = %q", d.uploadURL)
	}
	if d.publicBaseURL != "https://p.inari.site" {
		t.Errorf("publicBaseURL = %q", d.publicBaseURL)
	}
	if d.session == nil || d.read == nil {
		t.Fatal("clients are nil")
	}
	if d.session.Jar == nil {
		t.Fatal("session client must have a cookie jar")
	}
}

func TestPut_UploadsAndReturnsPathname(t *testing.T) {
	f := newFakeUpstream()
	ts := httptest.NewServer(f)
	defer ts.Close()

	d := newTestDriver(ts)
	res, err := d.Put("26-11/08/abc.png", "image/png", bytes.NewReader(testPNG))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if res == nil || res.Path != "26-11/08/abc123.png" {
		t.Errorf("Path = %v, want 26-11/08/abc123.png", res)
	}
	if res.ID != "26-11/08/abc123.png" {
		t.Errorf("ID = %q", res.ID)
	}
	if res.ETag != `"28f812512e9f334073eadbbad028dd3a"` {
		t.Errorf("ETag = %q", res.ETag)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.homeHits != 1 {
		t.Errorf("homepage hits = %d, want 1", f.homeHits)
	}
	if f.uploadHits != 1 {
		t.Errorf("upload hits = %d, want 1", f.uploadHits)
	}
	if f.lastFilename != "abc.png" {
		t.Errorf("filename = %q, want abc.png", f.lastFilename)
	}
	if !strings.HasPrefix(f.lastCookie, "session-"+f.lastToken) {
		t.Errorf("cookie and token are not a matched pair: cookie=%q token=%q", f.lastCookie, f.lastToken)
	}
}

func TestPut_MultipartEnvelopeExtractsFilePart(t *testing.T) {
	f := newFakeUpstream()
	ts := httptest.NewServer(f)
	defer ts.Close()

	d := newTestDriver(ts)

	// Build a multipart envelope exactly like a browser POST with a "file" part.
	var env bytes.Buffer
	mw := multipart.NewWriter(&env)
	fw, _ := mw.CreateFormFile("file", "photo.png")
	fw.Write(testPNG)
	mw.Close()

	res, err := d.Put("photo.png", mw.FormDataContentType(), bytes.NewReader(env.Bytes()))
	if err != nil {
		t.Fatalf("Put multipart envelope: %v", err)
	}
	if res == nil || res.Path == "" {
		t.Fatalf("res = %v", res)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	// The upstream must have received the clean image bytes (the fake rejects a
	// body that does not start with the PNG signature), and the original
	// filename from the envelope.
	if f.lastFilename != "photo.png" {
		t.Errorf("filename = %q, want photo.png (from the multipart envelope)", f.lastFilename)
	}
}

func TestPut_ReusesCachedSessionWithinTTL(t *testing.T) {
	f := newFakeUpstream()
	ts := httptest.NewServer(f)
	defer ts.Close()

	d := newTestDriver(ts)
	if _, err := d.Put("a.png", "image/png", bytes.NewReader(testPNG)); err != nil {
		t.Fatalf("first Put: %v", err)
	}
	if _, err := d.Put("b.png", "image/png", bytes.NewReader(testPNG)); err != nil {
		t.Fatalf("second Put: %v", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.homeHits != 1 {
		t.Errorf("homepage hits = %d, want 1 (session must be cached)", f.homeHits)
	}
	if f.uploadHits != 2 {
		t.Errorf("upload hits = %d, want 2", f.uploadHits)
	}
}

func TestPut_RefreshesSessionAfterTTL(t *testing.T) {
	f := newFakeUpstream()
	ts := httptest.NewServer(f)
	defer ts.Close()

	d := newTestDriver(ts)
	d.sessionTTL = time.Nanosecond // force expiry between calls
	if _, err := d.Put("a.png", "image/png", bytes.NewReader(testPNG)); err != nil {
		t.Fatalf("first Put: %v", err)
	}
	if _, err := d.Put("b.png", "image/png", bytes.NewReader(testPNG)); err != nil {
		t.Fatalf("second Put: %v", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.homeHits != 2 {
		t.Errorf("homepage hits = %d, want 2 (expired session must be re-fetched)", f.homeHits)
	}
	if f.uploadHits != 2 {
		t.Errorf("upload hits = %d, want 2", f.uploadHits)
	}
}

func TestPut_RetriesOn419(t *testing.T) {
	f := newFakeUpstream()
	ts := httptest.NewServer(f)
	defer ts.Close()

	d := newTestDriver(ts)
	d.sessionTTL = time.Nanosecond

	// First upload: session expired server-side → 419 → driver must re-fetch
	// and retry with a fresh pair. Use a seekable bytes.Reader so retry works.
	f.mu.Lock()
	f.failUploadWith419 = 1
	f.mu.Unlock()

	res, err := d.Put("a.png", "image/png", bytes.NewReader(testPNG))
	if err != nil {
		t.Fatalf("Put after 419: %v", err)
	}
	if res == nil || res.Path == "" {
		t.Fatalf("res = %v", res)
	}
	if f.homeHits < 2 {
		t.Errorf("homepage hits = %d, want >=2 (419 must trigger re-fetch)", f.homeHits)
	}
}

func TestPut_419WithoutSeekableBody(t *testing.T) {
	f := newFakeUpstream()
	ts := httptest.NewServer(f)
	defer ts.Close()

	d := newTestDriver(ts)
	d.sessionTTL = time.Nanosecond
	f.mu.Lock()
	f.failUploadWith419 = 1
	f.mu.Unlock()

	// Non-seekable reader: the retry must fail cleanly, not panic.
	_, err := d.Put("a.png", "image/png", io.NopCloser(bytes.NewReader(testPNG)))
	if err == nil {
		t.Fatal("expected error for 419 with non-seekable body")
	}
}

func TestPut_UpstreamNon200(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Write([]byte(strings.Replace(testHomepage, "%s", "tok-x", 1)))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"status":false,"message":"boom"}`))
	}))
	defer ts.Close()

	d := newTestDriver(ts)
	if _, err := d.Put("a.png", "image/png", bytes.NewReader(testPNG)); err == nil {
		t.Fatal("expected error for a 500 upstream")
	}
}

func TestGet_StreamsPublicObject(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/guest/26-11/08/abc.png" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(testPNG)
	}))
	defer ts.Close()

	d := newTestDriver(ts)
	d.publicBaseURL = ts.URL
	rc, size, ct, err := d.Get("26-11/08/abc.png")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, testPNG) {
		t.Errorf("body = %q, want %q", got, testPNG)
	}
	if size != int64(len(testPNG)) {
		t.Errorf("size = %d, want %d", size, len(testPNG))
	}
	if ct != "image/png" {
		t.Errorf("content type = %q", ct)
	}
}

func TestPublicURL_FormatsGuestPath(t *testing.T) {
	d := New()
	d.publicBaseURL = "https://p.inari.site"
	if got := d.PublicURL("26-11/08/a.png"); got != "https://p.inari.site/guest/26-11/08/a.png" {
		t.Errorf("PublicURL = %q", got)
	}
}

func TestDelete_NotSupported(t *testing.T) {
	d := New()
	if err := d.Delete("a.png"); err == nil {
		t.Fatal("expected delete to be unsupported")
	}
}

// ensure multipart helpers used above stay imported (compile-time guard).
var _ = multipart.ErrMessageTooLarge
