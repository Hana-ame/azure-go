package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Hana-ame/azure-go/storage"
	"github.com/gin-gonic/gin"
)

type mockDriver struct {
	putResult  *storage.PutResult
	putErr     error
	getBody    io.ReadCloser
	getLen     int64
	getType    string
	getErr     error
	deleteErr  error
	headLen    int64
	headType   string
	headErr    error

	deletedPaths []string
}

func (m *mockDriver) Put(path, contentType string, body io.Reader) (*storage.PutResult, error) {
	return m.putResult, m.putErr
}
func (m *mockDriver) Get(path string) (io.ReadCloser, int64, string, error) {
	return m.getBody, m.getLen, m.getType, m.getErr
}
func (m *mockDriver) Delete(path string) error {
	m.deletedPaths = append(m.deletedPaths, path)
	return m.deleteErr
}
func (m *mockDriver) Head(path string) (int64, string, error) {
	return m.headLen, m.headType, m.headErr
}

func setupTestRouter(driver storage.StorageDriver) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	s3 := NewS3Controller(driver)
	r.PUT("/:bucket/*key", s3.PutObject)
	r.GET("/:bucket/*key", s3.GetObject)
	r.DELETE("/:bucket/*key", s3.DeleteObject)
	r.HEAD("/:bucket/*key", s3.HeadObject)
	return r
}

func TestPutObject_Success(t *testing.T) {
	drv := &mockDriver{
		putResult: &storage.PutResult{ID: "id-1", ETag: `"abc123"`},
	}
	r := setupTestRouter(drv)

	body := bytes.NewBufferString("hello s3")
	req, _ := http.NewRequest("PUT", "/mybucket/photo.jpg", body)
	req.Header.Set("Content-Type", "image/jpeg")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if etag := w.Header().Get("ETag"); etag != `"abc123"` {
		t.Errorf("expected ETag abc123, got %s", etag)
	}
}

func TestPutObject_Dedup(t *testing.T) {
	drv := &mockDriver{
		putResult: &storage.PutResult{ID: "id-1", ETag: `"def456"`, DupPath: "oldbucket/file.txt"},
	}
	r := setupTestRouter(drv)

	body := bytes.NewBufferString("dup content")
	req, _ := http.NewRequest("PUT", "/mybucket/file2.txt", body)
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("x-amz-dedup-path") != "oldbucket/file.txt" {
		t.Errorf("expected x-amz-dedup-path, got %s", w.Header().Get("x-amz-dedup-path"))
	}
}

func TestPutObject_EmptyKey(t *testing.T) {
	drv := &mockDriver{}
	r := setupTestRouter(drv)

	req, _ := http.NewRequest("PUT", "/mybucket/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestGetObject_Success(t *testing.T) {
	content := "hello world"
	drv := &mockDriver{
		getBody: io.NopCloser(bytes.NewBufferString(content)),
		getLen:  int64(len(content)),
		getType: "text/plain",
	}
	r := setupTestRouter(drv)

	req, _ := http.NewRequest("GET", "/mybucket/hello.txt", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != content {
		t.Errorf("expected body %q, got %q", content, w.Body.String())
	}
	if etag := w.Header().Get("ETag"); etag == "" {
		t.Error("expected ETag header")
	}
}

func TestDeleteObject_Success(t *testing.T) {
	drv := &mockDriver{}
	r := setupTestRouter(drv)

	req, _ := http.NewRequest("DELETE", "/mybucket/photo.jpg", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
}

func TestHeadObject_Success(t *testing.T) {
	drv := &mockDriver{
		headLen:  1024,
		headType: "image/png",
	}
	r := setupTestRouter(drv)

	req, _ := http.NewRequest("HEAD", "/mybucket/photo.png", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "image/png" {
		t.Errorf("expected Content-Type image/png, got %s", w.Header().Get("Content-Type"))
	}
}
