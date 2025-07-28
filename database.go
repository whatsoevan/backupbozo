// bozobackup: Incremental, deduplicating photo/video backup tool with HTML reporting.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

func initDB(dbPath string) *sql.DB {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] Could not open database: %v\n", err)
		os.Exit(1)
	}
	sqlStmt := `
	CREATE TABLE IF NOT EXISTS files (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		src_path TEXT,
		dest_path TEXT,
		hash TEXT UNIQUE,
		size INTEGER,
		mtime INTEGER,
		copied_at TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_hash ON files(hash);
	`
	_, err = db.Exec(sqlStmt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] Could not initialize database schema: %v\n", err)
		db.Close()
		os.Exit(1)
	}
	return db
}

func fileAlreadyProcessed(db *sql.DB, hash string) bool {
	var id int
	err := db.QueryRow("SELECT id FROM files WHERE hash = ?", hash).Scan(&id)
	return err == nil
}

func insertFileRecord(db *sql.DB, src, dest, hash string, size, mtime int64) {
	_, err := db.Exec("INSERT OR IGNORE INTO files (src_path, dest_path, hash, size, mtime, copied_at) VALUES (?, ?, ?, ?, ?, ?)",
		src, dest, hash, size, mtime, time.Now().Format(time.RFC3339))
	if err != nil {
		log.Printf("DB insert error: %v", err)
	}
}

// getLastBackupTime returns the most recent copied_at time from the DB, or zero if none
func getLastBackupTime(db *sql.DB) (time.Time, error) {
	row := db.QueryRow("SELECT MAX(copied_at) FROM files WHERE copied_at IS NOT NULL")
	var last string
	err := row.Scan(&last)
	if err != nil || last == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, last)
	if err != nil {
		return time.Time{}, nil
	}
	return parsed, nil
}
