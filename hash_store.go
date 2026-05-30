package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/glebarez/go-sqlite"
)

// SQLiteHashStore implements storage.HashStore using SQLite.
type SQLiteHashStore struct {
	db *sql.DB
}

func NewSQLiteHashStore(dbPath string) (*SQLiteHashStore, error) {
	initDB := false
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		initDB = true
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open hash db: %w", err)
	}

	if initDB {
		_, err = db.Exec(`CREATE TABLE IF NOT EXISTS hashes (md5 TEXT PRIMARY KEY, path TEXT NOT NULL)`)
		if err != nil {
			return nil, fmt.Errorf("create hashes table: %w", err)
		}
	}

	return &SQLiteHashStore{db: db}, nil
}

func (s *SQLiteHashStore) Lookup(md5hex string) (string, error) {
	var path string
	err := s.db.QueryRow(`SELECT path FROM hashes WHERE md5 = ?`, md5hex).Scan(&path)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return path, err
}

func (s *SQLiteHashStore) Store(md5hex, path string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO hashes (md5, path) VALUES (?, ?)`, md5hex, path)
	return err
}
