// bozobackup: Basic tests for pipeline structures
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Test basic FileCandidate creation and metadata caching
func TestFileCandidate(t *testing.T) {
	// Create a test file
	tempDir := os.TempDir()
	testFile := filepath.Join(tempDir, "test.jpg")
	
	// Create the test file
	f, err := os.Create(testFile)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	f.WriteString("fake jpeg content")
	f.Close()
	defer os.Remove(testFile)
	
	// Test FileCandidate creation
	candidate, err := NewFileCandidate(testFile, "/backup")
	if err != nil {
		t.Fatalf("Failed to create FileCandidate: %v", err)
	}
	
	// Verify basic properties
	if candidate.Path != testFile {
		t.Errorf("Expected path %s, got %s", testFile, candidate.Path)
	}
	
	if candidate.Extension != ".jpg" {
		t.Errorf("Expected extension .jpg, got %s", candidate.Extension)
	}
	
	if candidate.DestDir != "/backup" {
		t.Errorf("Expected DestDir /backup, got %s", candidate.DestDir)
	}
	
	// Test date extraction (will use mtime fallback since fake JPEG)
	candidate.EnsureDate()
	if candidate.Date.IsZero() {
		t.Error("Expected date to be extracted (mtime fallback)")
	}
	
	// Test destination path computation
	err = candidate.EnsureDestPath()
	if err != nil {
		t.Errorf("Failed to compute destination path: %v", err)
	}
	
	expectedMonth := candidate.Date.Format("2006-01")
	expectedDest := filepath.Join("/backup", expectedMonth, "test.jpg")
	if candidate.DestPath != expectedDest {
		t.Errorf("Expected destination %s, got %s", expectedDest, candidate.DestPath)
	}
}

// Test FileState string representations
func TestFileStateStrings(t *testing.T) {
	tests := []struct {
		state    FileState
		expected string
	}{
		{StateCopied, "copied"},
		{StateSkippedExtension, "skipped (extension)"},
		{StateDuplicateHash, "duplicate (hash exists)"},
		{StateErrorCopy, "error (copy failed)"},
	}
	
	for _, test := range tests {
		if test.state.String() != test.expected {
			t.Errorf("Expected %s.String() = %s, got %s", 
				test.state, test.expected, test.state.String())
		}
	}
}

// Test ProcessingDecision and ProcessingResult workflow
func TestProcessingWorkflow(t *testing.T) {
	// Create test candidate
	tempDir := os.TempDir()
	testFile := filepath.Join(tempDir, "workflow.jpg")
	f, err := os.Create(testFile)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	f.Close()
	defer os.Remove(testFile)
	
	candidate, err := NewFileCandidate(testFile, "/backup")
	if err != nil {
		t.Fatalf("Failed to create candidate: %v", err)
	}
	
	// Create a processing decision
	decision := ProcessingDecision{
		State:         StateCopied,
		Reason:        "Test copy operation",
		ShouldCopy:    true,
		EstimatedSize: 100,
	}
	
	// Create processing result
	result := NewProcessingResult(candidate, decision)
	
	// Verify initial state
	if result.Candidate != candidate {
		t.Error("Result candidate should match input candidate")
	}
	
	if result.Decision.State != StateCopied {
		t.Error("Result decision state should match input decision")
	}
	
	if result.StartTime.IsZero() {
		t.Error("Start time should be set when result is created")
	}
	
	// Simulate processing completion
	time.Sleep(1 * time.Millisecond) // Small delay to test timing
	result.Complete(StateCopied, nil, 100)
	
	// Verify completion
	if result.EndTime.IsZero() {
		t.Error("End time should be set after completion")
	}
	
	if result.TimeTaken <= 0 {
		t.Error("Time taken should be positive")
	}
	
	if result.BytesCopied != 100 {
		t.Errorf("Expected 100 bytes copied, got %d", result.BytesCopied)
	}
	
	if !result.IsSuccess() {
		t.Error("Result should be marked as success")
	}
	
	if result.IsError() {
		t.Error("Result should not be marked as error")
	}
}

// Test error handling in ProcessingResult
func TestProcessingResultError(t *testing.T) {
	tempDir := os.TempDir()
	testFile := filepath.Join(tempDir, "error.jpg")
	f, err := os.Create(testFile)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	f.Close()
	defer os.Remove(testFile)
	
	candidate, _ := NewFileCandidate(testFile, "/backup")
	
	decision := ProcessingDecision{
		State:      StateErrorCopy,
		Reason:     "Test error",
		ShouldCopy: false,
	}
	
	result := NewProcessingResult(candidate, decision)
	testErr := fmt.Errorf("simulated copy error")
	result.Complete(StateErrorCopy, testErr, 0)
	
	if result.IsSuccess() {
		t.Error("Error result should not be marked as success")
	}
	
	if !result.IsError() {
		t.Error("Error result should be marked as error")
	}
	
	if result.Error != testErr {
		t.Error("Error should be preserved in result")
	}
}