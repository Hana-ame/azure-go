package main

import (
	"database/sql"
	"fmt"

	_ "github.com/glebarez/go-sqlite"
)

// SQLiteHashStore implements storage.HashStore using SQLite.
type SQLiteHashStore struct {
	db *sql.DB
}

func NewSQLiteHashStore(dbPath string) (*SQLiteHashStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open hash db: %w", err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS hashes (md5 TEXT PRIMARY KEY, path TEXT NOT NULL)`)
	if err != nil {
		return nil, fmt.Errorf("create hashes table: %w", err)
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

// SQLitePathStore implements storage.PathStore using SQLite.
type SQLitePathStore struct {
	db *sql.DB
}

func NewSQLitePathStore(dbPath string) (*SQLitePathStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open path db: %w", err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS paths (path TEXT PRIMARY KEY, id TEXT NOT NULL)`)
	if err != nil {
		return nil, fmt.Errorf("create paths table: %w", err)
	}

	return &SQLitePathStore{db: db}, nil
}

func (s *SQLitePathStore) Lookup(path string) (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM paths WHERE path = ?`, path).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

func (s *SQLitePathStore) Store(path, id string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO paths (path, id) VALUES (?, ?)`, path, id)
	return err
}

func (s *SQLitePathStore) Delete(path string) error {
	_, err := s.db.Exec(`DELETE FROM paths WHERE path = ?`, path)
	return err
}

// SQLiteR2Store maps hash -> R2 object key for R2 file delete lookups.
type SQLiteR2Store struct {
	db *sql.DB
}

func NewSQLiteR2Store(dbPath string) (*SQLiteR2Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open r2 db: %w", err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS r2_keys (hash TEXT PRIMARY KEY, path TEXT NOT NULL)`)
	if err != nil {
		return nil, fmt.Errorf("create r2_keys table: %w", err)
	}

	return &SQLiteR2Store{db: db}, nil
}

func (s *SQLiteR2Store) Lookup(hash string) (string, error) {
	var path string
	err := s.db.QueryRow(`SELECT path FROM r2_keys WHERE hash = ?`, hash).Scan(&path)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return path, err
}

func (s *SQLiteR2Store) Store(hash, path string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO r2_keys (hash, path) VALUES (?, ?)`, hash, path)
	return err
}

func (s *SQLiteR2Store) Delete(hash string) error {
	_, err := s.db.Exec(`DELETE FROM r2_keys WHERE hash = ?`, hash)
	return err
}
