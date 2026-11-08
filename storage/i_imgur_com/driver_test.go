package i_imgur_com

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var testPNG = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x01, 0x02, 0x03, 0x04}

// mustRead reads all of r, panicking on error (test helper).
func mustRead(r io.Reader) []byte {
	b, err := io.ReadAll(r)
	if err != nil {
		panic(err)
	}
	return b
}

func TestNew_ConfirmsConfig(t *testing.T) {
	d := New()
	if d.uploadURL != ImgurUploadURL {
		t.Errorf("uploadURL = %q", d.uploadURL)
	}
	if d.publicBase != ImgurPublicBase {
		t.Errorf("publicBase = %q", d.publicBase)
	}
	if d.client == nil {
		t.Fatal("client is nil")
	}
	if d.client.Transport == nil {
		t.Fatal("transport is nil")
	}
}

// okImgurResponse builds a valid imgur v3 success response.
func okImgurResponse(id string) []byte {
	b, _ := json.Marshal(map[string]any{
		"success": true,
		"data": map[string]any{
			"link":       "https://i.imgur.com/" + id + ".png",
			"deletehash": "abc123",
		},
	})
	return b
}

// TestPut_RawBodyUploadsAndReturnsLink verifies the raw-body path: the bytes
// are forwarded as multipart field "image" with type=file.
func TestPut_RawBodyUploadsAndReturnsLink(t *testing.T) {
	var gotBody []byte
	var gotField, gotType string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/3/image" {
			t.Errorf("path = %q, want /3/image", r.URL.Path)
		}
		if got := r.URL.Query().Get("client_id"); got != ImgurWebClientID {
			t.Errorf("client_id = %q", got)
		}
		mr, err := r.MultipartReader()
		if err != nil {
			t.Errorf("not multipart: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("read part: %v", err)
				break
			}
			switch part.FormName() {
			case "image":
				gotField = part.FormName()
				gotBody, _ = io.ReadAll(part)
			case "type":
				gotType = string(mustRead(part))
			}
			part.Close()
		}
		w.Write(okImgurResponse("AbCdEf"))
	}))
	defer ts.Close()

	d := New()
	d.uploadURL = ts.URL + "/3/image?client_id=" + ImgurWebClientID
	res, err := d.Put("x.png", "image/png", bytes.NewReader(testPNG))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !bytes.Equal(gotBody, testPNG) {
		t.Errorf("forwarded body = %q, want image bytes", gotBody)
	}
	if gotField != "image" {
		t.Errorf("field = %q, want image", gotField)
	}
	if gotType != "file" {
		t.Errorf("type field = %q, want file", gotType)
	}
	if res == nil || res.URL != "https://i.imgur.com/AbCdEf.png" {
		t.Errorf("URL = %v", res)
	}
	if res.Path != "AbCdEf.png" {
		t.Errorf("Path = %q, want AbCdEf.png (extension kept)", res.Path)
	}
	if res.ID != "AbCdEf" {
		t.Errorf("ID = %q, want AbCdEf", res.ID)
	}
	if res.DeleteURL == "" {
		t.Error("DeleteURL should be non-empty (deletehash present)")
	}
}

// TestPut_MultipartBody verifies the multipart-input path: a client posting a
// file part named "image" has it extracted and re-encapsulated.
func TestPut_MultipartBody(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("image", "photo.png")
	fw.Write(testPNG)
	mw.Close()

	var gotBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mr, _ := r.MultipartReader()
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}
			if part.FormName() == "image" {
				gotBody, _ = io.ReadAll(part)
			}
			part.Close()
		}
		w.Write(okImgurResponse("MmNnOo"))
	}))
	defer ts.Close()

	d := New()
	d.uploadURL = ts.URL + "/3/image"
	res, err := d.Put("", mw.FormDataContentType(), bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !bytes.Equal(gotBody, testPNG) {
		t.Errorf("forwarded body = %q, want image bytes", gotBody)
	}
	if res == nil || res.URL != "https://i.imgur.com/MmNnOo.png" {
		t.Errorf("URL = %v", res)
	}
	if res.Path != "MmNnOo.png" {
		t.Errorf("Path = %q, want MmNnOo.png (extension kept)", res.Path)
	}
}

func TestPut_UpstreamError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"success":false,"error":"invalid"}`))
	}))
	defer ts.Close()

	d := New()
	d.uploadURL = ts.URL + "/3/image"
	if _, err := d.Put("x.png", "image/png", bytes.NewReader(testPNG)); err == nil {
		t.Fatal("expected error for a 400 upstream")
	}
}

func TestPut_EmptyBody(t *testing.T) {
	d := New()
	if _, err := d.Put("x.png", "image/png", strings.NewReader("")); err == nil {
		t.Fatal("expected error for an empty body")
	}
}

func TestImgurIDFromLink(t *testing.T) {
	cases := map[string]string{
		"https://i.imgur.com/AbCdEf.png":  "AbCdEf",
		"https://i.imgur.com/AbCdEf.jpg":  "AbCdEf",
		"https://i.imgur.com/AbCdEf":      "AbCdEf",
		"https://i.imgur.com/AbCdEf.jpeg": "AbCdEf",
	}
	for in, want := range cases {
		if got := imgurIDFromLink(in); got != want {
			t.Errorf("imgurIDFromLink(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestImgurPathFromLink(t *testing.T) {
	cases := map[string]string{
		"https://i.imgur.com/AbCdEf.png":  "AbCdEf.png",
		"https://i.imgur.com/AbCdEf.jpg":  "AbCdEf.jpg",
		"https://i.imgur.com/AbCdEf":      "AbCdEf",
		"https://i.imgur.com/AbCdEf.jpeg": "AbCdEf.jpeg",
		"https://i.imgur.com/":            "",
	}
	for in, want := range cases {
		if got := imgurPathFromLink(in); got != want {
			t.Errorf("imgurPathFromLink(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestGet_StreamsImage verifies the proxy Get fetches the exact file from the
// public host and returns its bytes, size and content type.
func TestGet_StreamsImage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/AbCdEf.png" {
			t.Errorf("path = %q, want /AbCdEf.png", r.URL.Path)
		}
		if got := r.Header.Get("User-Agent"); got != browserUA {
			t.Errorf("User-Agent = %q, want browser UA", got)
		}
		if got := r.Header.Get("Accept"); got == "" {
			t.Error("Accept header should be set (imgur 429s bare requests)")
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(testPNG)
	}))
	defer ts.Close()

	d := New()
	d.publicBase = ts.URL
	rc, size, ct, err := d.Get("AbCdEf.png")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	if got := mustRead(rc); !bytes.Equal(got, testPNG) {
		t.Errorf("body = %q, want image bytes", got)
	}
	if size != int64(len(testPNG)) {
		t.Errorf("size = %d, want %d", size, len(testPNG))
	}
	if ct != "image/png" {
		t.Errorf("content-type = %q", ct)
	}
}

// TestGet_BareID verifies Get also works with a bare id (no extension).
func TestGet_BareID(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/AbCdEf" {
			t.Errorf("path = %q, want /AbCdEf", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(testPNG)
	}))
	defer ts.Close()

	d := New()
	d.publicBase = ts.URL
	if _, _, _, err := d.Get("AbCdEf"); err != nil {
		t.Fatalf("Get: %v", err)
	}
}

// TestGet_UpstreamError verifies a non-200 upstream surfaces as an error.
func TestGet_UpstreamError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	d := New()
	d.publicBase = ts.URL
	if _, _, _, err := d.Get("missing.png"); err == nil {
		t.Fatal("expected error for a 404 upstream")
	}
}
