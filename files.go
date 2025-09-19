// bozobackup: Incremental, deduplicating photo/video backup tool with HTML reporting.
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"bozobackup/metadata"
)

func getAllFiles(root string) ([]string, []error) {
	var files []string
	var errors []error
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			errors = append(errors, fmt.Errorf("%s: %v", path, err))
			return nil // continue walking
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files, errors
}

func getFileStat(path string) (int64, int64) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0
	}
	return info.Size(), info.ModTime().Unix()
}

// Global metadata extractor registry for efficient reuse
var metadataRegistry *metadata.ExtractorRegistry

func init() {
	metadataRegistry = metadata.NewExtractorRegistry()
}

// getFileDate extracts the best available date from a file using comprehensive metadata extraction
// This new implementation supports HEIC files, better EXIF handling, and multiple video metadata sources
func getFileDate(path string) time.Time {
	result := metadataRegistry.ExtractBestDate(path)
	
	// Always return a valid time, even if extraction failed
	if result.Error != nil || result.Date.IsZero() {
		// Final fallback to filesystem mtime
		if info, err := os.Stat(path); err == nil {
			return info.ModTime()
		}
		return time.Time{}
	}
	
	return result.Date
}

// getFileDateWithMetadata returns both the date and metadata information for detailed reporting
// This can be used when we want to know the confidence and source of the extracted date
func getFileDateWithMetadata(path string) (time.Time, metadata.MetadataResult) {
	result := metadataRegistry.ExtractBestDate(path)
	
	// Ensure we always have a valid date
	date := result.Date
	if result.Error != nil || date.IsZero() {
		if info, err := os.Stat(path); err == nil {
			date = info.ModTime()
			// Update result to reflect filesystem fallback
			if result.Error != nil {
				result.Date = date
				result.Confidence = metadata.ConfidenceLow
				result.Source = "Filesystem fallback after error"
				result.Error = nil
			}
		}
	}
	
	return date, result
}

// evaluateFileForBackup performs single-pass evaluation of a file for backup
// This replaces the duplicate logic between the two passes in backup.go
func evaluateFileForBackup(candidate *FileCandidate, db *sql.DB, incremental bool, minMtime int64) ProcessingDecision {
	// 1. Extension check (already computed in FileCandidate)
	if !allowedExtensions[candidate.Extension] {
		return ProcessingDecision{
			State:      StateSkippedExtension,
			Reason:     "Extension not allowed",
			ShouldCopy: false,
		}
	}
	
	// 2. Incremental check (info already cached in FileCandidate)
	if incremental && minMtime > 0 && candidate.Info.ModTime().Unix() <= minMtime {
		return ProcessingDecision{
			State:      StateSkippedIncremental,
			Reason:     "File older than last backup",
			ShouldCopy: false,
		}
	}
	
	// 3. Date check (uses cached extraction from metadata system)
	candidate.EnsureDate()
	if candidate.DateErr != nil || candidate.Date.IsZero() {
		return ProcessingDecision{
			State:      StateSkippedDate,
			Reason:     "No valid date found",
			ShouldCopy: false,
		}
	}
	
	// 4. Destination path and existence check
	err := candidate.EnsureDestPath()
	if err != nil {
		return ProcessingDecision{
			State:      StateErrorDate,
			Reason:     fmt.Sprintf("Failed to determine destination: %v", err),
			ShouldCopy: false,
		}
	}
	
	// Create destination directory
	destMonthDir := filepath.Dir(candidate.DestPath)
	os.MkdirAll(destMonthDir, 0755)
	
	// Check if destination file already exists
	if _, err := os.Stat(candidate.DestPath); err == nil {
		return ProcessingDecision{
			State:      StateSkippedDestExists,
			Reason:     "File already exists at destination",
			ShouldCopy: false,
		}
	}
	
	// 5. Hash computation and duplicate check (only for files that pass all other checks)
	candidate.EnsureHash()
	if candidate.HashErr != nil {
		return ProcessingDecision{
			State:      StateErrorHash,
			Reason:     "Failed to compute file hash",
			ShouldCopy: false,
		}
	}
	
	// Check for hash duplicates in database
	if fileAlreadyProcessed(db, candidate.Hash) {
		return ProcessingDecision{
			State:      StateDuplicateHash,
			Reason:     "File hash already exists in database",
			ShouldCopy: false,
		}
	}
	
	// File should be copied!
	return ProcessingDecision{
		State:         StateCopied,
		Reason:        "File ready for backup",
		ShouldCopy:    true,
		EstimatedSize: candidate.Info.Size(),
	}
}

func getFileHash(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// copyFileAtomic copies a file to a temp file in the destination directory, then renames it atomically.
// If interrupted (ctx.Done), the temp file is deleted and the destination is never partially written.
func copyFileAtomic(ctx context.Context, src, dst string) error {
	tmpDst := dst + ".tmp"
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(tmpDst)
	if err != nil {
		return err
	}
	defer func() {
		out.Close()
		if ctx.Err() != nil {
			os.Remove(tmpDst)
		}
	}()

	buf := make([]byte, 1024*1024)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, readErr := in.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		os.Remove(tmpDst)
		return ctx.Err()
	}
	return os.Rename(tmpDst, dst)
}

// TimestampInfo contains file timestamp information for preservation
type TimestampInfo struct {
	ModTime time.Time // File modification time
	ATime   time.Time // File access time (when available)
}

// getFileTimestamps extracts timestamp information from a file
func getFileTimestamps(path string) (TimestampInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return TimestampInfo{}, fmt.Errorf("failed to stat file %s: %w", path, err)
	}
	
	timestamps := TimestampInfo{
		ModTime: info.ModTime(),
		ATime:   info.ModTime(), // Default atime to mtime if unavailable
	}
	
	// Try to get more precise timestamps from syscall (Unix/Linux)
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		// Access time from syscall
		timestamps.ATime = time.Unix(stat.Atim.Sec, stat.Atim.Nsec)
	}
	
	return timestamps, nil
}

// setFileTimestamps sets the timestamps on a file
func setFileTimestamps(path string, timestamps TimestampInfo) error {
	// Use os.Chtimes to set modification and access times
	err := os.Chtimes(path, timestamps.ATime, timestamps.ModTime)
	if err != nil {
		return fmt.Errorf("failed to set timestamps on %s: %w", path, err)
	}
	return nil
}

// verifyTimestamps checks that timestamps were preserved correctly
func verifyTimestamps(path string, expectedTimestamps TimestampInfo) error {
	actualTimestamps, err := getFileTimestamps(path)
	if err != nil {
		return fmt.Errorf("failed to verify timestamps: %w", err)
	}
	
	// Allow small tolerance for timestamp precision differences (1 second)
	const tolerance = time.Second
	
	if actualTimestamps.ModTime.Sub(expectedTimestamps.ModTime).Abs() > tolerance {
		return fmt.Errorf("modification time not preserved: expected %v, got %v", 
			expectedTimestamps.ModTime, actualTimestamps.ModTime)
	}
	
	return nil
}

// copyFileWithTimestamps copies a file atomically while preserving all original timestamps.
// This replaces copyFileAtomic to fix the critical data loss issue where creation dates were lost.
//
// The function:
// 1. Extracts source file timestamps before copying
// 2. Performs atomic copy using temporary file (same as copyFileAtomic)
// 3. Preserves original timestamps on the destination file
// 4. Verifies timestamps were set correctly
// 5. Handles context cancellation properly (cleans up temp files)
func copyFileWithTimestamps(ctx context.Context, src, dst string) error {
	// Step 1: Extract source file timestamps before any operations
	sourceTimestamps, err := getFileTimestamps(src)
	if err != nil {
		return fmt.Errorf("failed to get source timestamps: %w", err)
	}
	
	// Step 2: Perform atomic file copy (same logic as copyFileAtomic)
	tmpDst := dst + ".tmp"
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(tmpDst)
	if err != nil {
		return fmt.Errorf("failed to create temp file %s: %w", tmpDst, err)
	}
	
	// Ensure cleanup on error or cancellation
	defer func() {
		out.Close()
		if ctx.Err() != nil {
			os.Remove(tmpDst)
		}
	}()

	// Copy data with context cancellation support
	buf := make([]byte, 1024*1024) // 1MB buffer for efficient copying
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		
		n, readErr := in.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("failed to write to temp file: %w", writeErr)
			}
		}
		
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("failed to read from source file: %w", readErr)
		}
	}
	
	// Ensure data is written to disk
	if err := out.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	
	// Close temp file before setting timestamps
	if err := out.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	
	// Check for cancellation before final operations
	if ctx.Err() != nil {
		os.Remove(tmpDst)
		return ctx.Err()
	}
	
	// Step 3: Set timestamps on temp file before rename
	// This ensures the destination file has correct timestamps from the moment it appears
	if err := setFileTimestamps(tmpDst, sourceTimestamps); err != nil {
		os.Remove(tmpDst)
		return fmt.Errorf("failed to set timestamps on temp file: %w", err)
	}
	
	// Step 4: Atomically move temp file to final destination
	if err := os.Rename(tmpDst, dst); err != nil {
		os.Remove(tmpDst)
		return fmt.Errorf("failed to rename temp file to destination: %w", err)
	}
	
	// Step 5: Verify timestamps were preserved correctly
	if err := verifyTimestamps(dst, sourceTimestamps); err != nil {
		// Non-fatal error - file was copied successfully but timestamps may not be perfect
		// Log warning but don't fail the entire operation
		fmt.Printf("Warning: %v\n", err)
	}
	
	return nil
}

func checkDirExists(path string, label string) {
	info, err := os.Stat(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] %s directory '%s' does not exist: %v\n", label, path, err)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "[FATAL] %s path '%s' is not a directory\n", label, path)
		os.Exit(1)
	}
}
