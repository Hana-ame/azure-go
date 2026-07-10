package main

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/joho/godotenv"
)

func TestS3Integration(t *testing.T) {
	skipIfNoCreds(t)

	// Setup temporary DBs
	tmpDir := t.TempDir()
	hashDB := tmpDir + "/hashes.db"
	pathDB := tmpDir + "/paths.db"

	hs, err := NewSQLiteHashStore(hashDB)
	if err != nil {
		t.Fatalf("failed to create hash store: %v", err)
	}
	ps, err := NewSQLitePathStore(pathDB)
	if err != nil {
		t.Fatalf("failed to create path store: %v", err)
	}

	// Setup Agent
	godotenv.Load("refresh_token")
	agent := &Agent{
		tenent_id:     os.Getenv("tenent_id"),
		client_id:     os.Getenv("client_id"),
		redirect_url:  os.Getenv("redirect_url"),
		client_secret: os.Getenv("client_secret"),
		scope:         os.Getenv("scope"),
		access_token:  accessToken(),
		refresh_token: os.Getenv("refresh_token"),
	}

	driver := NewAzureDriver(agent, hs, ps)

	// 1. Test Basic Put -> Get -> Head -> Delete
	testPath := "testbucket/testfile.txt"
	testContent := "hello azure s3 integration"
	contentType := "text/plain"

	t.Run("BasicCycle", func(t *testing.T) {
		// Put
		res, err := driver.Put(testPath, contentType, bytes.NewBufferString(testContent))
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
		if res.ID == "" {
			t.Fatal("Put result ID is empty")
		}

		// Verify PathStore has the ID
		id, err := ps.Lookup(testPath)
		if err != nil || id != res.ID {
			t.Errorf("PathStore lookup failed: got %s, want %s, err: %v", id, res.ID, err)
		}

		// Head
		length, ctype, err := driver.Head(testPath)
		if err != nil {
			t.Fatalf("Head failed: %v", err)
		}
		if length != int64(len(testContent)) {
			t.Errorf("Head length mismatch: got %d, want %d", length, len(testContent))
		}
		if ctype != contentType {
			t.Errorf("Head type mismatch: got %s, want %s", ctype, contentType)
		}

		// Get
		body, length, ctype, err := driver.Get(testPath)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		defer body.Close()
		gotContent, _ := io.ReadAll(body)
		if string(gotContent) != testContent {
			t.Errorf("Get content mismatch: got %q, want %q", string(gotContent), testContent)
		}
		if length != int64(len(testContent)) || ctype != contentType {
			t.Errorf("Get metadata mismatch: len %d, type %s", length, ctype)
		}

		// Delete
		err = driver.Delete(testPath)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		// Verify deleted in PathStore
		id, err = ps.Lookup(testPath)
		if id != "" {
			t.Errorf("PathStore should have deleted the path, but got id %s", id)
		}
	})

	// 2. Test Deduplication with PathStore
	t.Run("Deduplication", func(t *testing.T) {
		path1 := "bucket1/file1.txt"
		path2 := "bucket2/file2.txt"
		content := "identical content for dedup"

		// First upload
		_, err = driver.Put(path1, "text/plain", bytes.NewBufferString(content))
		if err != nil {
			t.Fatalf("Put1 failed: %v", err)
		}

		// Second upload (same content)
		res2, err := driver.Put(path2, "text/plain", bytes.NewBufferString(content))
		if err != nil {
			t.Fatalf("Put2 failed: %v", err)
		}

		if res2.DupPath == "" {
			t.Error("expected DupPath to be set for identical content")
		}
		if res2.DupPath != path1 {
			t.Errorf("expected DupPath %s, got %s", path1, res2.DupPath)
		}

		// Crucially: path2 should now map to the SAME ID as path1
		id1, _ := ps.Lookup(path1)
		id2, _ := ps.Lookup(path2)
		if id1 == "" || id1 != id2 {
			t.Errorf("PathStore should map both paths to same ID: id1=%s, id2=%s", id1, id2)
		}
	})
}
