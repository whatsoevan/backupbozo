// bozobackup: File processing pipeline structures for Phase 1 refactor
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
)

// FileState represents the explicit state of a file during processing
// This eliminates the classification ambiguity between passes
type FileState int

const (
	// File was successfully copied
	StateCopied FileState = iota

	// File was skipped for various reasons
	StateSkippedExtension   // Extension not in allowedExtensions
	StateSkippedIncremental // File older than last backup (incremental mode)
	StateSkippedDate        // Could not extract valid date from file
	StateSkippedDestExists  // Destination file already exists

	// File is a duplicate based on hash
	StateDuplicateHash // Hash already exists in database

	// Errors during processing
	StateErrorStat // Error calling os.Stat()
	StateErrorDate // Error extracting date metadata
	StateErrorHash // Error computing file hash
	StateErrorCopy // Error copying file
	StateErrorWalk // Error during directory walking
)

// String returns human-readable state names for reporting
func (s FileState) String() string {
	switch s {
	case StateCopied:
		return "copied"
	case StateSkippedExtension:
		return "skipped (extension)"
	case StateSkippedIncremental:
		return "skipped (incremental)"
	case StateSkippedDate:
		return "skipped (no date)"
	case StateSkippedDestExists:
		return "skipped (destination exists)"
	case StateDuplicateHash:
		return "duplicate (hash exists)"
	case StateErrorStat:
		return "error (stat)"
	case StateErrorDate:
		return "error (date extraction)"
	case StateErrorHash:
		return "error (hash computation)"
	case StateErrorCopy:
		return "error (copy failed)"
	case StateErrorWalk:
		return "error (walk failed)"
	default:
		return "unknown"
	}
}

// FileCandidate represents a file being evaluated for backup
type FileCandidate struct {
	// Basic file information
	Path      string      // Full source path
	Info      os.FileInfo // Cached os.Stat() result (expensive, called once)
	Extension string      // Normalized lowercase extension (e.g., ".jpg")

	// Destination information
	DestDir  string // Base destination directory
	DestPath string // Full computed destination path (YYYY-MM/filename)
}

// FileResult tracks the outcome of file operations in a simplified way
type FileResult struct {
	Path        string    // Source file path
	DestPath    string    // Destination file path (for reporting)
	State       FileState // Final processing state
	Error       error     // Any error that occurred during processing
	BytesCopied int64     // Actual bytes copied (0 if skipped/error)
}

// classifyAndProcessFile performs unified file classification and processing
// Returns a FileResult with the outcome of processing
func classifyAndProcessFile(ctx context.Context, candidate *FileCandidate, db *sql.DB, hashSet map[string]bool, batchInserter *BatchInserter, incremental bool, minMtime int64) *FileResult {
	// Get processing state using evaluation logic
	state := evaluateFileForBackup(candidate, db, hashSet, incremental, minMtime)

	// If state is not StateCopied, we're done - no copy needed
	if state != StateCopied {
		return &FileResult{
			Path:        candidate.Path,
			DestPath:    candidate.DestPath,
			State:       state,
			Error:       nil,
			BytesCopied: 0,
		}
	}

	// State is StateCopied - attempt the actual copy operation
	var finalState FileState = StateCopied
	var bytesCopied int64 = 0
	var copyErr error

	if ctx.Err() != nil {
		// Context cancelled before we could copy
		finalState = StateErrorCopy
		copyErr = ctx.Err()
	} else {
		// Use streaming copy that computes hash during copy for maximum efficiency
		hash, streamErr := copyFileWithHash(ctx, candidate.Path, candidate.DestPath)
		if streamErr != nil {
			finalState = StateErrorCopy
			copyErr = streamErr
		} else {
			// Copy succeeded - add to batch inserter
			batchInserter.Add(candidate.Path, candidate.DestPath, hash,
				candidate.Info.Size(), candidate.Info.ModTime().Unix())
			finalState = StateCopied
			bytesCopied = candidate.Info.Size()
		}
	}

	return &FileResult{
		Path:        candidate.Path,
		DestPath:    candidate.DestPath,
		State:       finalState,
		Error:       copyErr,
		BytesCopied: bytesCopied,
	}
}

// AccountingSummary provides accounting from FileResult collection
type AccountingSummary struct {
	// Counts by final state
	Copied     int
	Skipped    int
	Duplicates int
	Errors     int

	// File lists for HTML report generation
	CopiedFiles    [][2]string   // [src, dst] pairs
	SkippedFiles   []SkippedFile // Files skipped with reasons
	DuplicateFiles [][2]string   // [src, dst] pairs for duplicates
	ErrorList      []string      // Error messages

	// Statistics
	TotalBytes int64 // Total bytes copied
	TotalFiles int   // Total files processed
	WalkErrors int   // Directory walking errors
}

// SkippedFile represents a file that was skipped during backup
type SkippedFile struct {
	Path   string
	Reason string
}

// GenerateAccountingSummary creates a complete accounting summary from FileResult collection
func GenerateAccountingSummary(results []*FileResult, walkErrors []error) AccountingSummary {
	summary := AccountingSummary{
		TotalFiles: len(results),
		WalkErrors: len(walkErrors),
	}

	// Process each result and categorize by state
	for _, result := range results {
		// Skip nil results (can happen when context is cancelled during processing)
		if result == nil {
			continue
		}
		switch result.State {
		case StateCopied:
			summary.Copied++
			summary.CopiedFiles = append(summary.CopiedFiles, [2]string{
				result.Path,
				result.DestPath,
			})
			summary.TotalBytes += result.BytesCopied

		case StateDuplicateHash:
			summary.Duplicates++
			summary.DuplicateFiles = append(summary.DuplicateFiles, [2]string{
				result.Path,
				result.DestPath,
			})

		case StateSkippedExtension, StateSkippedIncremental, StateSkippedDate, StateSkippedDestExists:
			summary.Skipped++
			summary.SkippedFiles = append(summary.SkippedFiles, SkippedFile{
				Path:   result.Path,
				Reason: result.State.String(),
			})

		case StateErrorStat, StateErrorDate, StateErrorHash, StateErrorCopy:
			summary.Errors++
			errorMsg := fmt.Sprintf("%s: %v", result.Path, result.Error)
			if result.Error == nil {
				errorMsg = fmt.Sprintf("%s: %s", result.Path, result.State.String())
			}
			summary.ErrorList = append(summary.ErrorList, errorMsg)

		case StateErrorWalk:
			// Walk errors are handled separately in walkErrors parameter
			summary.Errors++
		}
	}

	// Add walk errors to error list
	for _, walkErr := range walkErrors {
		summary.ErrorList = append(summary.ErrorList, fmt.Sprintf("walk error: %v", walkErr))
	}
	summary.Errors += len(walkErrors)

	return summary
}
