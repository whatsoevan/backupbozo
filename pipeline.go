// bozobackup: File processing pipeline structures for Phase 1 refactor
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileState represents the explicit state of a file during processing
// This eliminates the classification ambiguity between passes
type FileState int

const (
	// File was successfully copied
	StateCopied FileState = iota
	
	// File was skipped for various reasons
	StateSkippedExtension    // Extension not in allowedExtensions
	StateSkippedIncremental  // File older than last backup (incremental mode)
	StateSkippedDate         // Could not extract valid date from file
	StateSkippedDestExists   // Destination file already exists
	
	// File is a duplicate based on hash
	StateDuplicateHash       // Hash already exists in database
	
	// Errors during processing
	StateErrorStat          // Error calling os.Stat()
	StateErrorDate          // Error extracting date metadata
	StateErrorHash          // Error computing file hash
	StateErrorCopy          // Error copying file
	StateErrorWalk          // Error during directory walking
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
// This structure caches expensive operations like os.Stat() and date extraction
// to eliminate redundant I/O between the two-pass system
type FileCandidate struct {
	// Basic file information
	Path      string      // Full source path
	Info      os.FileInfo // Cached os.Stat() result (expensive, called once)
	Extension string      // Normalized lowercase extension (e.g., ".jpg")
	
	// Extracted metadata (cached to avoid expensive re-computation)
	Date     time.Time // Extracted from EXIF/video metadata or mtime fallback
	DateErr  error     // Any error from date extraction
	
	// Destination information
	DestDir  string // Base destination directory
	DestPath string // Full computed destination path (YYYY-MM/filename)
	
	// Processing metadata
	Hash    string // SHA256 hash (computed when needed)
	HashErr error  // Any error from hash computation
}

// NewFileCandidate creates a FileCandidate with basic information populated
// This performs the expensive os.Stat() call once and caches the result
func NewFileCandidate(path, destDir string) (*FileCandidate, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	
	ext := strings.ToLower(filepath.Ext(path))
	
	candidate := &FileCandidate{
		Path:      path,
		Info:      info,
		Extension: ext,
		DestDir:   destDir,
	}
	
	return candidate, nil
}

// EnsureDate extracts and caches the file date if not already done
// This is expensive for video files (ffprobe) so we cache the result
func (fc *FileCandidate) EnsureDate() {
	if !fc.Date.IsZero() || fc.DateErr != nil {
		return // Already extracted
	}
	
	fc.Date, fc.DateErr = fc.extractFileDate()
}

// EnsureDestPath computes and caches the destination path based on extracted date
func (fc *FileCandidate) EnsureDestPath() error {
	if fc.DestPath != "" {
		return nil // Already computed
	}
	
	fc.EnsureDate()
	if fc.DateErr != nil {
		return fc.DateErr
	}
	
	if fc.Date.IsZero() {
		return nil // No valid date, can't compute destination
	}
	
	monthFolder := fc.Date.Format("2006-01")
	destMonthDir := filepath.Join(fc.DestDir, monthFolder)
	fc.DestPath = filepath.Join(destMonthDir, filepath.Base(fc.Path))
	
	return nil
}

// EnsureHash computes and caches the file hash if not already done
func (fc *FileCandidate) EnsureHash() {
	if fc.Hash != "" || fc.HashErr != nil {
		return // Already computed
	}
	
	fc.Hash = getFileHash(fc.Path)
	if fc.Hash == "" {
		fc.HashErr = fmt.Errorf("failed to compute hash")
	}
}

// extractFileDate performs the actual date extraction logic
// This is the same logic as the current getFileDate() function
func (fc *FileCandidate) extractFileDate() (time.Time, error) {
	// Try EXIF for JPEG files
	if fc.Extension == ".jpg" || fc.Extension == ".jpeg" {
		if dt, err := getExifDate(fc.Path); err == nil {
			return dt, nil
		}
	}
	
	// Try video metadata for video files
	if fc.Extension == ".mp4" || fc.Extension == ".mov" || fc.Extension == ".mkv" || 
	   fc.Extension == ".webm" || fc.Extension == ".avi" {
		if dt, err := getVideoCreationDate(fc.Path); err == nil {
			return dt, nil
		}
	}
	
	// Fallback to file modification time
	return fc.Info.ModTime(), nil
}

// ProcessingDecision encapsulates the decision of whether/how a file should be processed
// This eliminates the inconsistent classification between passes
type ProcessingDecision struct {
	State      FileState // Explicit state (copied, skipped, duplicate, error)
	Reason     string    // Human-readable explanation for reporting
	ShouldCopy bool      // Clear boolean: should this file be copied?
	
	// Additional context for decision
	Priority   int       // Processing priority (for future parallel processing)
	EstimatedSize int64  // Expected bytes to copy (for progress estimation)
}

// ProcessingResult tracks the outcome of file operations
// This replaces the scattered outcome tracking in multiple arrays
type ProcessingResult struct {
	Candidate   *FileCandidate // Original file candidate
	Decision    ProcessingDecision // Decision that was made
	FinalState  FileState     // Actual final state after processing
	
	// Execution details
	Error        error         // Any error that occurred during processing
	BytesCopied  int64        // Actual bytes copied (0 if skipped/error)
	TimeTaken    time.Duration // Time spent processing this file
	
	// Database tracking
	DBInserted   bool         // Whether file record was inserted into DB
	
	// Timestamps
	StartTime    time.Time    // When processing started
	EndTime      time.Time    // When processing completed
}

// NewProcessingResult creates a result with timing started
func NewProcessingResult(candidate *FileCandidate, decision ProcessingDecision) *ProcessingResult {
	return &ProcessingResult{
		Candidate:  candidate,
		Decision:   decision,
		FinalState: decision.State, // Initialize with decision state
		StartTime:  time.Now(),
	}
}

// Complete marks the result as finished and records timing
func (pr *ProcessingResult) Complete(finalState FileState, err error, bytesCopied int64) {
	pr.EndTime = time.Now()
	pr.TimeTaken = pr.EndTime.Sub(pr.StartTime)
	pr.FinalState = finalState
	pr.Error = err
	pr.BytesCopied = bytesCopied
}

// IsSuccess returns true if the file was processed successfully (copied or legitimately skipped)
func (pr *ProcessingResult) IsSuccess() bool {
	switch pr.FinalState {
	case StateCopied, StateSkippedExtension, StateSkippedIncremental, 
		 StateSkippedDate, StateSkippedDestExists, StateDuplicateHash:
		return true
	default:
		return false
	}
}

// IsError returns true if processing failed due to an error
func (pr *ProcessingResult) IsError() bool {
	switch pr.FinalState {
	case StateErrorStat, StateErrorDate, StateErrorHash, StateErrorCopy, StateErrorWalk:
		return true
	default:
		return false
	}
}