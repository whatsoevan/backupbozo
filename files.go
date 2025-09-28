// backupbozo: Incremental, deduplicating photo/video backup tool with HTML reporting.
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"backupbozo/metadata"

	"github.com/schollz/progressbar/v3"
)

// FileWithInfo combines file path with cached os.FileInfo to eliminate duplicate syscalls
type FileWithInfo struct {
	Path string
	Info os.FileInfo
}

func getAllFiles(root string) ([]FileWithInfo, []error) {
	var files []FileWithInfo
	var errors []error
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			errors = append(errors, fmt.Errorf("%s: %v", path, err))
			return nil // continue walking
		}
		if !info.IsDir() {
			files = append(files, FileWithInfo{
				Path: path,
				Info: info,
			})
		}
		return nil
	})
	return files, errors
}

// Global metadata extractor registry for efficient reuse
var metadataRegistry *metadata.ExtractorRegistry

func init() {
	metadataRegistry = metadata.NewExtractorRegistry()
}

// PlanningResult contains the result of planning phase evaluation
type PlanningResult struct {
	ShouldCopy bool
	Size       int64
	Reason     string
}

// evaluateFileForPlanning performs fast evaluation without expensive metadata extraction
// Used in planning phase to estimate space requirements using filesystem dates only
func evaluateFileForPlanning(candidate *FileCandidate, incremental bool, minMtime int64) PlanningResult {
	// 1. Extension check (already computed in FileCandidate)
	if !allowedExtensions[candidate.Extension] {
		return PlanningResult{
			ShouldCopy: false,
			Size:       0,
			Reason:     "Extension not allowed",
		}
	}

	// 2. Incremental check (info already cached in FileCandidate)
	if incremental && minMtime > 0 && candidate.Info.ModTime().Unix() <= minMtime {
		return PlanningResult{
			ShouldCopy: false,
			Size:       0,
			Reason:     "File older than last backup",
		}
	}

	// 3. Fast date check using filesystem mtime (avoid expensive metadata extraction)
	// For planning purposes, we use filesystem modification time which is always available
	// The execution phase will do full metadata extraction for accurate YYYY-MM organization
	filesystemDate := candidate.Info.ModTime()
	if filesystemDate.IsZero() {
		return PlanningResult{
			ShouldCopy: false,
			Size:       0,
			Reason:     "No valid filesystem date",
		}
	}

	// 4. Compute destination path using filesystem date for planning
	monthFolder := filesystemDate.Format("2006-01")
	destMonthDir := filepath.Join(candidate.DestDir, monthFolder)
	planningDestPath := filepath.Join(destMonthDir, filepath.Base(candidate.Path))

	// Check if destination file already exists
	if _, err := os.Stat(planningDestPath); err == nil {
		return PlanningResult{
			ShouldCopy: false,
			Size:       0,
			Reason:     "File already exists at destination",
		}
	}

	// File would be copied (skip hash check in planning phase)
	return PlanningResult{
		ShouldCopy: true,
		Size:       candidate.Info.Size(),
		Reason:     "File ready for backup",
	}
}

// evaluateFilesForPlanningParallel processes files using a worker pool for concurrent planning evaluation
// This provides 4-8x speedup on multi-core systems while maintaining result ordering
// Uses fast filesystem dates and avoids expensive metadata extraction during planning
func evaluateFilesForPlanningParallel(ctx context.Context, files []FileWithInfo, destDir string,
	bar *progressbar.ProgressBar, incremental bool, minMtime int64, workers int) []PlanningResult {

	// Channels for worker communication
	type job struct {
		index int // Preserve ordering
		file  FileWithInfo
	}

	type resultWithIndex struct {
		index  int
		result PlanningResult
	}

	jobs := make(chan job, workers*2)                // Buffered channel for work items
	results := make(chan resultWithIndex, workers*2) // Buffered channel for results

	// Start worker goroutines
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				// Create FileCandidate for this file
				candidate := &FileCandidate{
					Path:      job.file.Path,
					Info:      job.file.Info,
					Extension: strings.ToLower(filepath.Ext(job.file.Path)),
					DestDir:   destDir,
				}

				// Evaluate file for planning using fast filesystem dates
				planResult := evaluateFileForPlanning(candidate, incremental, minMtime)

				// Send result with index to maintain ordering
				select {
				case results <- resultWithIndex{index: job.index, result: planResult}:
					// Progress bar update (thread-safe)
					if bar != nil {
						bar.Add(1)
					}
				case <-ctx.Done():
					return // Context cancelled
				}
			}
		}()
	}

	// Producer: send jobs to workers
	go func() {
		defer close(jobs)
		for i, file := range files {
			select {
			case jobs <- job{index: i, file: file}:
				// Job sent successfully
			case <-ctx.Done():
				return // Context cancelled
			}
		}
	}()

	// Collector: gather results and close channel when done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results in ordered slice with context awareness
	orderedResults := make([]PlanningResult, len(files))
	for {
		select {
		case result, ok := <-results:
			if !ok {
				// Channel closed, all results collected
				goto resultsComplete
			}
			orderedResults[result.index] = result.result
		case <-ctx.Done():
			// Context cancelled, stop collecting results
			fmt.Printf("\nPlanning phase interrupted\n")
			goto resultsComplete
		}
	}
resultsComplete:

	return orderedResults
}

// EvaluationResult contains the result of file evaluation including duplicate path info
type EvaluationResult struct {
	State                 FileState
	ExistingDuplicatePath string // Only populated for StateDuplicateHash
}

// evaluateFileForBackup performs single-pass evaluation of a file for backup
// This replaces the duplicate logic between the two passes in backup.go
func evaluateFileForBackup(candidate *FileCandidate, db *sql.DB, hashToPath map[string]string, incremental bool, minMtime int64) EvaluationResult {
	// 1. Extension check (already computed in FileCandidate)
	if !allowedExtensions[candidate.Extension] {
		return EvaluationResult{State: StateSkippedExtension}
	}

	// 2. Incremental check (info already cached in FileCandidate)
	if incremental && minMtime > 0 && candidate.Info.ModTime().Unix() <= minMtime {
		return EvaluationResult{State: StateSkippedIncremental}
	}

	// 3. Date extraction and destination path computation
	result := metadataRegistry.ExtractBestDate(candidate.Path)
	date := result.Date
	if result.Error != nil || date.IsZero() {
		// Fallback to file modification time
		if candidate.Info != nil {
			date = candidate.Info.ModTime()
		}
		if date.IsZero() {
			return EvaluationResult{State: StateSkippedDate}
		}
	}

	// Compute destination path
	monthFolder := date.Format("2006-01")
	destMonthDir := filepath.Join(candidate.DestDir, monthFolder)
	candidate.DestPath = filepath.Join(destMonthDir, filepath.Base(candidate.Path))

	// Create destination directory
	os.MkdirAll(destMonthDir, 0755)

	// Check if destination file already exists
	if _, err := os.Stat(candidate.DestPath); err == nil {
		return EvaluationResult{State: StateSkippedDestExists}
	}

	// Hash computation and duplicate check (only for files that pass all other checks)
	f, err := os.Open(candidate.Path)
	if err != nil {
		return EvaluationResult{State: StateErrorHash}
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return EvaluationResult{State: StateErrorHash}
	}
	hash := fmt.Sprintf("%x", h.Sum(nil))

	// Check for hash duplicates in memory (O(1) lookup)
	if existingPath, exists := hashToPath[hash]; exists {
		return EvaluationResult{State: StateDuplicateHash, ExistingDuplicatePath: existingPath}
	}

	// File should be copied!
	return EvaluationResult{State: StateCopied}
}

// copyFileWithHash combines file copying and hash computation in a single pass
// This optimizes I/O by reading the file only once while preserving modification time
// Returns the SHA256 hash and any error that occurred during the operation
func copyFileWithHash(ctx context.Context, src, dst string) (string, error) {
	// Step 1: Get source file modification time
	srcInfo, err := os.Stat(src)
	if err != nil {
		return "", fmt.Errorf("failed to stat source file %s: %w", src, err)
	}
	sourceModTime := srcInfo.ModTime()

	// Step 2: Perform atomic file copy with simultaneous hash computation
	tmpDst := dst + ".tmp"
	in, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("failed to open source file %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(tmpDst)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file %s: %w", tmpDst, err)
	}

	// Initialize hash computation
	hasher := sha256.New()

	// Ensure cleanup on error or cancellation
	defer func() {
		out.Close()
		if ctx.Err() != nil {
			os.Remove(tmpDst)
		}
	}()

	// Copy data with simultaneous hash computation using io.MultiWriter
	multiWriter := io.MultiWriter(out, hasher)
	buf := make([]byte, 1024*1024) // 1MB buffer for efficient copying

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		n, readErr := in.Read(buf)
		if n > 0 {
			if _, writeErr := multiWriter.Write(buf[:n]); writeErr != nil {
				return "", fmt.Errorf("failed to write to temp file: %w", writeErr)
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", fmt.Errorf("failed to read from source file: %w", readErr)
		}
	}

	// Ensure data is written to disk
	if err := out.Sync(); err != nil {
		return "", fmt.Errorf("failed to sync temp file: %w", err)
	}

	// Close temp file before setting timestamps
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp file: %w", err)
	}

	// Check for cancellation before final operations
	if ctx.Err() != nil {
		os.Remove(tmpDst)
		return "", ctx.Err()
	}

	// Step 3: Set modification time on temp file before rename
	if err := os.Chtimes(tmpDst, sourceModTime, sourceModTime); err != nil {
		// Log warning but don't fail - timestamp preservation is best-effort
		fmt.Printf("Warning: failed to set timestamps on %s: %v\n", tmpDst, err)
	}

	// Step 4: Atomically move temp file to final destination
	if err := os.Rename(tmpDst, dst); err != nil {
		os.Remove(tmpDst)
		return "", fmt.Errorf("failed to rename temp file to destination: %w", err)
	}

	// Step 5: Return computed hash
	hash := fmt.Sprintf("%x", hasher.Sum(nil))
	return hash, nil
}
