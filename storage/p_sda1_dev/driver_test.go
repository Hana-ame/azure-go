package p_sda1_dev

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// pngHeader is a valid PNG signature (psda1proxy.ValidateImage accepts it).
var pngHeader = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x01, 0x02, 0x03, 0x04}

// webpHeader is a valid RIFF....WEBP header.
var webpHeader = []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")

func makeEnvelope(partName, filename string, payload []byte) []byte {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if filename != "" {
		fw, err := w.CreateFormFile(partName, filename)
		if err != nil {
			panic(err)
		}
		if _, err := fw.Write(payload); err != nil {
			panic(err)
		}
	} else {
		fw, err := w.CreateFormField(partName)
		if err != nil {
			panic(err)
		}
		if _, err := fw.Write(payload); err != nil {
			panic(err)
		}
	}
	if err := w.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// --- StripEnvelope: reading back legacy objects ---

func TestStripEnvelope_RemovesOneLayer(t *testing.T) {
	envelope := makeEnvelope("image", "1792228850/photo.png", pngHeader)

	clean, ok := StripEnvelope(envelope)
	if !ok {
		t.Fatal("expected the envelope to be recognised")
	}
	if !bytes.Equal(clean, pngHeader) {
		t.Errorf("stripped = %q, want the image bytes %q", clean, pngHeader)
	}
	if bytes.Contains(clean, []byte("Content-Disposition")) {
		t.Error("stripped bytes still contain envelope headers")
	}
}

func TestStripEnvelope_RemovesNestedLayers(t *testing.T) {
	inner := makeEnvelope("image", "a.png", pngHeader)
	outer := makeEnvelope("image", "b.png", inner)

	clean, ok := StripEnvelope(outer)
	if !ok {
		t.Fatal("expected the nested envelope to be recognised")
	}
	if !bytes.Equal(clean, pngHeader) {
		t.Errorf("stripped = %q, want the image bytes %q", clean, pngHeader)
	}
}

func TestStripEnvelope_PassesThroughCleanImage(t *testing.T) {
	clean, ok := StripEnvelope(pngHeader)
	if ok {
		t.Error("expected a clean image not to be treated as an envelope")
	}
	if !bytes.Equal(clean, pngHeader) {
		t.Errorf("bytes changed: %q", clean)
	}
}

func TestStripEnvelope_PassesThroughPlainText(t *testing.T) {
	plain := []byte("just some text, not an envelope")
	clean, ok := StripEnvelope(plain)
	if ok {
		t.Error("expected plain text not to be treated as an envelope")
	}
	if !bytes.Equal(clean, plain) {
		t.Errorf("bytes changed: %q", clean)
	}
}

func TestStripEnvelope_IgnoresBinaryContainingBoundarySequence(t *testing.T) {
	payload := append(append([]byte{}, pngHeader...), []byte("\r\n--not-a-boundary\r\n")...)
	clean, ok := StripEnvelope(payload)
	if ok {
		t.Error("expected image data to pass through, it only looks envelope-ish")
	}
	if !bytes.Equal(clean, payload) {
		t.Errorf("bytes changed: %q", clean)
	}
}

func TestStripEnvelope_ShortInput(t *testing.T) {
	for _, s := range []string{"--", "--a", "--a\r\n", "ab"} {
		clean, ok := StripEnvelope([]byte(s))
		if ok {
			t.Errorf("%q: expected no strip", s)
		}
		if !bytes.Equal(clean, []byte(s)) {
			t.Errorf("%q: bytes changed", s)
		}
	}
}

// --- Put: raw streaming directly to upstream ---

func testDriver(ts *httptest.Server) *Driver {
	return &Driver{
		baseURL: ts.URL,
		apiURL:  ts.URL + "/api/v1/upload_external_noform",
		client:  http.DefaultClient,
	}
}

func okUploadResponse(path string) []byte {
	b, err := json.Marshal(map[string]any{
		"success": true,
		"data":    map[string]any{"size": 12, "path": "/" + path, "url": "https://p.sda1.dev/" + path},
	})
	if err != nil {
		panic(err)
	}
	return b
}

func TestPut_PostsRawBodyDirectly(t *testing.T) {
	var gotBody []byte
	var gotCT, gotFilename string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		gotCT = r.Header.Get("Content-Type")
		gotFilename = r.URL.Query().Get("filename")
		w.Write(okUploadResponse("1792228850/photo.png"))
	}))
	defer ts.Close()

	res, err := testDriver(ts).Put("1792228850/photo.png", "image/png", bytes.NewReader(pngHeader))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if !bytes.Equal(gotBody, pngHeader) {
		t.Errorf("upstream received %d bytes starting %q, want exactly the image bytes",
			len(gotBody), gotBody[:min(len(gotBody), 24)])
	}
	if gotCT != "image/png" {
		t.Errorf("content type = %q, want image/png", gotCT)
	}
	if gotFilename != "photo.png" {
		t.Errorf("filename query = %q, want \"photo.png\"", gotFilename)
	}

	if res == nil || res.Path != "1792228850/photo.png" {
		t.Errorf("PutResult.Path = %v, want 1792228850/photo.png", res)
	}
	if res.ID != res.Path {
		t.Errorf("PutResult.ID = %q, want it to equal Path", res.ID)
	}
	sum := md5.Sum(pngHeader)
	wantETag := `"` + hex.EncodeToString(sum[:]) + `"`
	if res.ETag != wantETag {
		t.Errorf("PutResult.ETag = %q, want md5 of the image bytes %q", res.ETag, wantETag)
	}
}

func TestPut_RejectsUpstreamError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"success":false,"message":"no image"}`))
	}))
	defer ts.Close()

	if _, err := testDriver(ts).Put("1/x.png", "image/png", bytes.NewReader(pngHeader)); err == nil {
		t.Fatal("expected an error for a non-200 upstream response")
	}
}

func TestPut_UpstreamSuccessFalse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":false,"message":"unsupported format"}`))
	}))
	defer ts.Close()

	if _, err := testDriver(ts).Put("1/x.png", "image/png", bytes.NewReader(pngHeader)); err == nil {
		t.Fatal("expected an error when the upstream reports failure")
	}
}

// --- Get: legacy envelope objects are stripped, clean objects pass through ---

func TestGet_StripsLegacyEnvelope(t *testing.T) {
	envelope := makeEnvelope("image", "1792228850/photo.png", pngHeader)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(envelope)
	}))
	defer ts.Close()

	rc, size, ct, err := testDriver(ts).Get("1792228850/photo.png")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !bytes.Equal(got, pngHeader) {
		t.Errorf("got %q, want the image bytes %q", got, pngHeader)
	}
	if size != int64(len(pngHeader)) {
		t.Errorf("size = %d, want %d", size, len(pngHeader))
	}
	if ct != "image/png" {
		t.Errorf("content type = %q, want image/png", ct)
	}
}

func TestGet_PassesCleanBodyThroughStreaming(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngHeader)
	}))
	defer ts.Close()

	rc, size, ct, err := testDriver(ts).Get("1792228850/photo.png")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)

	if !bytes.Equal(got, pngHeader) {
		t.Errorf("got %q, want %q", got, pngHeader)
	}
	if size != int64(len(pngHeader)) {
		t.Errorf("size = %d, want %d", size, len(pngHeader))
	}
	if ct != "image/png" {
		t.Errorf("content type = %q, want image/png", ct)
	}
}

func TestGet_404(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	if _, _, _, err := testDriver(ts).Get("1/missing.png"); err == nil {
		t.Fatal("expected an error for a 404 upstream")
	}
}
