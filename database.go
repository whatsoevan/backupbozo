// backupbozo: Incremental, deduplicating photo/video backup tool with HTML reporting.
package main

import (
	"context"
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
	db         *sql.DB
	hashToPath map[string]string
	records    []FileRecord
	mutex      sync.Mutex
	batchSize  int
}

// NewBatchInserter creates a new batch inserter
func NewBatchInserter(db *sql.DB, hashToPath map[string]string, batchSize int) *BatchInserter {
	if batchSize <= 0 {
		batchSize = 1000 // Default batch size
	}
	return &BatchInserter{
		db:         db,
		hashToPath: hashToPath,
		records:    make([]FileRecord, 0, batchSize),
		batchSize:  batchSize,
	}
}

// Add adds a file record to the batch
func (bi *BatchInserter) Add(src, dest, hash string, size, mtime int64) {
	bi.mutex.Lock()
	defer bi.mutex.Unlock()

	// Add to hash map immediately for duplicate detection
	bi.hashToPath[hash] = dest

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
		bi.flushUnsafeWithContext(context.Background())
	}
}

// Flush flushes any remaining records to the database
func (bi *BatchInserter) Flush() {
	bi.FlushWithContext(context.Background())
}

// FlushWithContext flushes any remaining records to the database with context cancellation support
func (bi *BatchInserter) FlushWithContext(ctx context.Context) {
	bi.mutex.Lock()
	defer bi.mutex.Unlock()
	bi.flushUnsafeWithContext(ctx)
}

// flushUnsafe flushes records without locking (caller must hold mutex)
func (bi *BatchInserter) flushUnsafe() {
	bi.flushUnsafeWithContext(context.Background())
}

// flushUnsafeWithContext flushes records without locking and with context cancellation support
func (bi *BatchInserter) flushUnsafeWithContext(ctx context.Context) {
	if len(bi.records) == 0 {
		return
	}

	// Check if context is already cancelled before starting
	if ctx.Err() != nil {
		log.Printf("Batch insert: context cancelled, skipping flush")
		return
	}

	tx, err := bi.db.Begin()
	if err != nil {
		log.Printf("Batch insert: failed to begin transaction: %v", err)
		return
	}

	// Check context after beginning transaction
	if ctx.Err() != nil {
		log.Printf("Batch insert: context cancelled during transaction begin")
		tx.Rollback()
		return
	}

	stmt, err := tx.Prepare("INSERT OR IGNORE INTO files (src_path, dest_path, hash, size, mtime, copied_at) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		log.Printf("Batch insert: failed to prepare statement: %v", err)
		tx.Rollback()
		return
	}
	defer stmt.Close()

	// Insert records with periodic context checks
	for i, record := range bi.records {
		// Check context every 100 records to avoid excessive overhead
		if i%100 == 0 && ctx.Err() != nil {
			log.Printf("Batch insert: context cancelled during execution at record %d", i)
			tx.Rollback()
			return
		}

		_, err := stmt.Exec(record.SrcPath, record.DestPath, record.Hash, record.Size, record.Mtime, record.CopiedAt)
		if err != nil {
			log.Printf("Batch insert: failed to execute statement: %v", err)
		}
	}

	// Final context check before commit
	if ctx.Err() != nil {
		log.Printf("Batch insert: context cancelled before commit")
		tx.Rollback()
		return
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

// loadExistingHashes loads all existing file hashes from the database into a map for O(1) lookup
// This eliminates the need for per-file database queries during duplicate detection
func loadExistingHashes(db *sql.DB) map[string]string {
	hashToPath := make(map[string]string)

	rows, err := db.Query("SELECT hash, dest_path FROM files WHERE hash IS NOT NULL")
	if err != nil {
		log.Printf("Warning: Could not load existing hashes: %v", err)
		return hashToPath
	}
	defer rows.Close()

	for rows.Next() {
		var hash, destPath string
		if err := rows.Scan(&hash, &destPath); err != nil {
			log.Printf("Warning: Error scanning hash and path: %v", err)
			continue
		}
		hashToPath[hash] = destPath
	}

	if err := rows.Err(); err != nil {
		log.Printf("Warning: Error iterating hashes: %v", err)
	}

	log.Printf("Loaded %d existing hashes into memory", len(hashToPath))
	return hashToPath
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
