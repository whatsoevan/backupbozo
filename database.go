// bozobackup: Incremental, deduplicating photo/video backup tool with HTML reporting.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// FileRecord represents a file record for batch insertion
type FileRecord struct {
	SrcPath  string
	DestPath string
	Hash     string
	Size     int64
	Mtime    int64
	CopiedAt string
}

// BatchInserter handles batch insertion of file records for performance
type BatchInserter struct {
	db       *sql.DB
	hashSet  map[string]bool
	records  []FileRecord
	mutex    sync.Mutex
	batchSize int
}

// NewBatchInserter creates a new batch inserter
func NewBatchInserter(db *sql.DB, hashSet map[string]bool, batchSize int) *BatchInserter {
	if batchSize <= 0 {
		batchSize = 1000 // Default batch size
	}
	return &BatchInserter{
		db:        db,
		hashSet:   hashSet,
		records:   make([]FileRecord, 0, batchSize),
		batchSize: batchSize,
	}
}

// Add adds a file record to the batch
func (bi *BatchInserter) Add(src, dest, hash string, size, mtime int64) {
	bi.mutex.Lock()
	defer bi.mutex.Unlock()

	// Add to hash set immediately for duplicate detection
	bi.hashSet[hash] = true

	// Add to batch
	bi.records = append(bi.records, FileRecord{
		SrcPath:  src,
		DestPath: dest,
		Hash:     hash,
		Size:     size,
		Mtime:    mtime,
		CopiedAt: time.Now().Format(time.RFC3339),
	})

	// Flush if batch is full
	if len(bi.records) >= bi.batchSize {
		bi.flushUnsafe()
	}
}

// Flush flushes any remaining records to the database
func (bi *BatchInserter) Flush() {
	bi.mutex.Lock()
	defer bi.mutex.Unlock()
	bi.flushUnsafe()
}

// flushUnsafe flushes records without locking (caller must hold mutex)
func (bi *BatchInserter) flushUnsafe() {
	if len(bi.records) == 0 {
		return
	}

	tx, err := bi.db.Begin()
	if err != nil {
		log.Printf("Batch insert: failed to begin transaction: %v", err)
		return
	}

	stmt, err := tx.Prepare("INSERT OR IGNORE INTO files (src_path, dest_path, hash, size, mtime, copied_at) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		log.Printf("Batch insert: failed to prepare statement: %v", err)
		tx.Rollback()
		return
	}
	defer stmt.Close()

	for _, record := range bi.records {
		_, err := stmt.Exec(record.SrcPath, record.DestPath, record.Hash, record.Size, record.Mtime, record.CopiedAt)
		if err != nil {
			log.Printf("Batch insert: failed to execute statement: %v", err)
		}
	}

	err = tx.Commit()
	if err != nil {
		log.Printf("Batch insert: failed to commit transaction: %v", err)
		tx.Rollback()
	} else {
		log.Printf("Batch inserted %d records", len(bi.records))
	}

	// Clear the batch
	bi.records = bi.records[:0]
}

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

// loadExistingHashes loads all existing file hashes from the database into a map for O(1) lookup
// This eliminates the need for per-file database queries during duplicate detection
func loadExistingHashes(db *sql.DB) map[string]bool {
	hashSet := make(map[string]bool)

	rows, err := db.Query("SELECT hash FROM files WHERE hash IS NOT NULL")
	if err != nil {
		log.Printf("Warning: Could not load existing hashes: %v", err)
		return hashSet
	}
	defer rows.Close()

	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			log.Printf("Warning: Error scanning hash: %v", err)
			continue
		}
		hashSet[hash] = true
	}

	if err := rows.Err(); err != nil {
		log.Printf("Warning: Error iterating hashes: %v", err)
	}

	log.Printf("Loaded %d existing hashes into memory", len(hashSet))
	return hashSet
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
