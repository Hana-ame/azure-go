package p_sda1_dev

import (
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	d := New()
	if d == nil {
		t.Fatal("New() returned nil")
	}
	if d.baseURL != "https://p.sda1.dev" {
		t.Errorf("baseURL = %q, want %q", d.baseURL, "https://p.sda1.dev")
	}
}

func TestPutGetDeleteHead(t *testing.T) {
	d := New()
	body := strings.NewReader("test content for p.sda1.dev")
	result, err := d.Put("test.txt", "text/plain", body)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if result.Path == "" {
		t.Fatal("Put returned empty Path")
	}
	t.Logf("Put result: ID=%s Path=%s", result.ID, result.Path)

	// delete should fail
	err = d.Delete(result.Path)
	if err == nil {
		t.Log("Delete succeeded (unexpected)")
	} else {
		t.Logf("Delete expectedly failed: %v", err)
	}

	// head
	size, ct, err := d.Head(result.Path)
	if err != nil {
		t.Fatalf("Head failed: %v", err)
	}
	t.Logf("Head: size=%d contentType=%s", size, ct)

	// get
	rc, gotSize, gotCT, err := d.Get(result.Path)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer rc.Close()
	t.Logf("Get: size=%d contentType=%s", gotSize, gotCT)
}
