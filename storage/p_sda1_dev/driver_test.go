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
var pngHeader = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x04, 0x01, 0x02, 0x03, 0x04}

// webpHeader is a valid RIFF....WEBP header.
var webpHeader = []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")

// makeEnvelope wraps payload in a multipart/form-data envelope the way the old
// driver did: field name "image", a filename, and the file bytes inside.
// filename == "" builds a plain form field instead (CreateFormField), which
// has no filename attribute.
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

// boundaryOf recovers the boundary from the first line of an envelope.
func boundaryOf(envelope []byte) string {
	line := string(envelope)
	if i := strings.Index(line, "\r\n"); i >= 0 {
		return strings.TrimPrefix(line[:i], "--")
	}
	return ""
}

// --- extractBody: what the driver sends upstream ---

func TestExtractBody_PassesThroughRawBody(t *testing.T) {
	out, ct, err := extractBody("image/png", bytes.NewReader(pngHeader))
	if err != nil {
		t.Fatalf("extractBody: %v", err)
	}
	if got, _ := io.ReadAll(out); !bytes.Equal(got, pngHeader) {
		t.Errorf("bytes = %v, want untouched %v", got, pngHeader)
	}
	if ct != "image/png" {
		t.Errorf("content type = %q, want image/png", ct)
	}
}

func TestExtractBody_PassesThroughWhenContentTypeEmpty(t *testing.T) {
	out, ct, err := extractBody("", bytes.NewReader(pngHeader))
	if err != nil {
		t.Fatalf("extractBody: %v", err)
	}
	if got, _ := io.ReadAll(out); !bytes.Equal(got, pngHeader) {
		t.Errorf("bytes = %v, want untouched %v", got, pngHeader)
	}
	if ct != "application/octet-stream" {
		t.Errorf("content type = %q, want application/octet-stream", ct)
	}
}

func TestExtractBody_PassesThroughWhenNoBoundary(t *testing.T) {
	// Declared multipart but no boundary we can parse with: pass through rather
	// than guess at an envelope layout.
	envelope := makeEnvelope("image", "f.png", pngHeader)
	out, ct, err := extractBody("multipart/form-data", bytes.NewReader(envelope))
	if err != nil {
		t.Fatalf("extractBody: %v", err)
	}
	if got, _ := io.ReadAll(out); !bytes.Equal(got, envelope) {
		t.Error("expected the envelope to pass through unchanged")
	}
	// A multipart Content-Type must never leave with raw body bytes — that
	// would reintroduce exactly the envelope bug.
	if ct != "application/octet-stream" {
		t.Errorf("content type = %q, want application/octet-stream", ct)
	}
}

func TestExtractBody_UnwrapsMultipartEnvelope(t *testing.T) {
	// The regression: the old driver fabricated an envelope around clean image
	// bytes, p.sda1.dev stored the envelope, and reads returned it instead of an
	// image.
	envelope := makeEnvelope("image", "1792228850/photo.png", pngHeader)

	out, gotCT, err := extractBody("multipart/form-data; boundary="+boundaryOf(envelope),
		bytes.NewReader(envelope))
	if err != nil {
		t.Fatalf("extractBody: %v", err)
	}
	got, _ := io.ReadAll(out)
	if !bytes.Equal(got, pngHeader) {
		t.Errorf("unwrapped bytes = %q, want the original image bytes %q", got, pngHeader)
	}
	if !bytes.HasPrefix(got, []byte{0x89, 'P', 'N', 'G'}) {
		t.Errorf("unwrapped bytes do not start with the PNG signature: %q", got[:8])
	}
	if gotCT != "application/octet-stream" {
		t.Errorf("content type = %q, want application/octet-stream from the part", gotCT)
	}
}

func TestExtractBody_PrefersFilePartOverTextField(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if fw, err := w.CreateFormField("note"); err != nil {
		panic(err)
	} else if _, err := fw.Write([]byte("hello")); err != nil {
		panic(err)
	}
	if fw, err := w.CreateFormFile("image", "1792228850/a.webp"); err != nil {
		panic(err)
	} else if _, err := fw.Write(webpHeader); err != nil {
		panic(err)
	}
	if err := w.Close(); err != nil {
		panic(err)
	}

	out, _, err := extractBody(w.FormDataContentType(), bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("extractBody: %v", err)
	}
	if got, _ := io.ReadAll(out); !bytes.Equal(got, webpHeader) {
		t.Errorf("unwrapped = %q, want the file part %q (not the text field)", got, webpHeader)
	}
}

func TestExtractBody_UnwrapsFieldNameWithoutFilename(t *testing.T) {
	// A part created with CreateFormField has no filename; "image" is still the
	// field name this driver has always used, so it must be picked.
	envelope := makeEnvelope("image", "", pngHeader)

	out, _, err := extractBody("multipart/form-data; boundary="+boundaryOf(envelope),
		bytes.NewReader(envelope))
	if err != nil {
		t.Fatalf("extractBody: %v", err)
	}
	if got, _ := io.ReadAll(out); !bytes.Equal(got, pngHeader) {
		t.Errorf("unwrapped = %q, want %q", got, pngHeader)
	}
}

func TestExtractBody_PrefersImageNameWhenNeitherPartHasFilename(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if fw, err := w.CreateFormField("note"); err != nil {
		panic(err)
	} else if _, err := fw.Write([]byte("text")); err != nil {
		panic(err)
	}
	if fw, err := w.CreateFormField("image"); err != nil {
		panic(err)
	} else if _, err := fw.Write(pngHeader); err != nil {
		panic(err)
	}
	if err := w.Close(); err != nil {
		panic(err)
	}

	out, _, err := extractBody(w.FormDataContentType(), bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("extractBody: %v", err)
	}
	if got, _ := io.ReadAll(out); !bytes.Equal(got, pngHeader) {
		t.Errorf("unwrapped = %q, want the part named \"image\"", got)
	}
}

func TestExtractBody_EmptyMultipartErrors(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.Close(); err != nil {
		panic(err)
	}
	if _, _, err := extractBody(w.FormDataContentType(), bytes.NewReader(buf.Bytes())); err == nil {
		t.Fatal("expected an error for a multipart body with no parts")
	}
}

func TestNormaliseContentType(t *testing.T) {
	cases := map[string]string{
		"image/webp":                        "image/webp",
		"":                                  "application/octet-stream",
		"multipart/form-data; boundary=abc": "application/octet-stream",
		"multipart/form-data":               "application/octet-stream",
		"text/plain; charset=utf-8":         "text/plain; charset=utf-8",
	}
	for in, want := range cases {
		if got := normaliseContentType(in); got != want {
			t.Errorf("normaliseContentType(%q) = %q, want %q", in, got, want)
		}
	}
}

// --- StripEnvelope: reading back objects the old driver stored ---

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
	// A doubly-wrapped object: envelope around envelope around the image.
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
	// Real image data can contain "\r\n--"; it must never be mis-sliced. The
	// guard is that an envelope must START with "--<boundary>\r\n".
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

// --- Put: the bytes that actually leave the process ---

// testDriver wires a Driver at an httptest server standing in for p.sda1.dev.
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

func TestPut_PostsRawBodyNotEnvelope(t *testing.T) {
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
	if bytes.Contains(gotBody, []byte("Content-Disposition")) {
		t.Error("upstream received a multipart envelope, not the file")
	}
	if gotCT != "image/png" {
		t.Errorf("content type = %q, want image/png", gotCT)
	}
	if gotFilename != "photo.png" {
		// filename must be the last path segment, not the whole ts/name path.
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

func TestPut_UnwrapsIncomingMultipart(t *testing.T) {
	// Defence in depth: a caller that hands the driver an envelope must still
	// result in clean bytes upstream.
	envelope := makeEnvelope("image", "1792228850/photo.png", pngHeader)

	var gotBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		w.Write(okUploadResponse("1792228850/photo.png"))
	}))
	defer ts.Close()

	if _, err := testDriver(ts).Put("1792228850/photo.png",
		"multipart/form-data; boundary="+boundaryOf(envelope), bytes.NewReader(envelope)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if !bytes.Equal(gotBody, pngHeader) {
		t.Errorf("upstream received %q, want the image bytes %q", gotBody, pngHeader)
	}
	if bytes.Contains(gotBody, []byte("Content-Disposition")) {
		t.Error("upstream still received an envelope")
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
	// An object the old driver stored: the envelope, with the header lying about
	// its content type.
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
	if bytes.Contains(got, []byte("Content-Disposition")) {
		t.Error("body still contains the envelope")
	}
	if size != int64(len(pngHeader)) {
		t.Errorf("size = %d, want %d (the clean body, not the envelope)", size, len(pngHeader))
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
