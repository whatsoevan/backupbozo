// bozobackup: Resume capability with state file tracking
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ResumeState tracks the progress of a backup operation
type ResumeState struct {
	StateFilePath   string
	ProcessedFiles  map[string]bool // Set of files that have been processed
	StartTime       time.Time
	SourceDir       string
	DestDir         string
	Incremental     bool
}

// NewResumeState creates a new resume state for the given backup operation
func NewResumeState(srcDir, destDir string, incremental bool) *ResumeState {
	stateFileName := fmt.Sprintf("bozobackup_resume_%s.state", time.Now().Format("20060102_150405"))
	stateFilePath := filepath.Join(destDir, stateFileName)

	return &ResumeState{
		StateFilePath:  stateFilePath,
		ProcessedFiles: make(map[string]bool),
		StartTime:      time.Now(),
		SourceDir:      srcDir,
		DestDir:        destDir,
		Incremental:    incremental,
	}
}

// LoadResumeState loads an existing resume state from a state file
func LoadResumeState(stateFilePath string) (*ResumeState, error) {
	file, err := os.Open(stateFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open state file: %w", err)
	}
	defer file.Close()

	rs := &ResumeState{
		StateFilePath:  stateFilePath,
		ProcessedFiles: make(map[string]bool),
	}

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		line := scanner.Text()
		lineNum++

		if lineNum == 1 {
			// First line: metadata (start time, src, dest, incremental)
			var startTimeStr, incremental string
			n, err := fmt.Sscanf(line, "START_TIME:%s SRC:%s DEST:%s INCREMENTAL:%s",
				&startTimeStr, &rs.SourceDir, &rs.DestDir, &incremental)
			if err != nil || n != 4 {
				return nil, fmt.Errorf("invalid state file format on line %d", lineNum)
			}

			rs.StartTime, err = time.Parse(time.RFC3339, startTimeStr)
			if err != nil {
				return nil, fmt.Errorf("invalid start time format: %w", err)
			}

			rs.Incremental = (incremental == "true")
		} else {
			// Subsequent lines: processed file paths
			if line != "" {
				rs.ProcessedFiles[line] = true
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading state file: %w", err)
	}

	return rs, nil
}

// MarkFileProcessed adds a file to the processed set and updates the state file
func (rs *ResumeState) MarkFileProcessed(filePath string) error {
	rs.ProcessedFiles[filePath] = true
	return rs.writeStateFile()
}

// IsFileProcessed checks if a file has already been processed
func (rs *ResumeState) IsFileProcessed(filePath string) bool {
	return rs.ProcessedFiles[filePath]
}

// writeStateFile writes the current state to disk
func (rs *ResumeState) writeStateFile() error {
	file, err := os.Create(rs.StateFilePath)
	if err != nil {
		return fmt.Errorf("failed to create state file: %w", err)
	}
	defer file.Close()

	// Write metadata header
	incrementalStr := "false"
	if rs.Incremental {
		incrementalStr = "true"
	}

	_, err = fmt.Fprintf(file, "START_TIME:%s SRC:%s DEST:%s INCREMENTAL:%s\n",
		rs.StartTime.Format(time.RFC3339), rs.SourceDir, rs.DestDir, incrementalStr)
	if err != nil {
		return fmt.Errorf("failed to write state file header: %w", err)
	}

	// Write processed file paths
	for filePath := range rs.ProcessedFiles {
		_, err = fmt.Fprintf(file, "%s\n", filePath)
		if err != nil {
			return fmt.Errorf("failed to write processed file to state: %w", err)
		}
	}

	return nil
}

// CleanupStateFile removes the state file (called on successful completion)
func (rs *ResumeState) CleanupStateFile() error {
	return os.Remove(rs.StateFilePath)
}

// GetProgress returns statistics about the current progress
func (rs *ResumeState) GetProgress() (processed int, duration time.Duration) {
	return len(rs.ProcessedFiles), time.Since(rs.StartTime)
}

// FindResumeStateFiles finds existing resume state files in the destination directory
func FindResumeStateFiles(destDir string) ([]string, error) {
	pattern := filepath.Join(destDir, "bozobackup_resume_*.state")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to search for resume files: %w", err)
	}
	return matches, nil
}