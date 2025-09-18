// bozobackup: Tests for timestamp preservation functionality
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestTimestampExtraction tests the getFileTimestamps function
func TestTimestampExtraction(t *testing.T) {
	// Create test file with known modification time
	tempDir := os.TempDir()
	testFile := filepath.Join(tempDir, "timestamp_test.txt")
	
	// Create file and set specific modification time
	f, err := os.Create(testFile)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	f.WriteString("test content for timestamp preservation")
	f.Close()
	defer os.Remove(testFile)
	
	// Set a specific modification time (2023-01-15 12:30:45)
	testTime := time.Date(2023, 1, 15, 12, 30, 45, 0, time.UTC)
	err = os.Chtimes(testFile, testTime, testTime)
	if err != nil {
		t.Fatalf("Failed to set test file timestamps: %v", err)
	}
	
	// Extract timestamps
	timestamps, err := getFileTimestamps(testFile)
	if err != nil {
		t.Fatalf("Failed to extract timestamps: %v", err)
	}
	
	// Verify modification time is preserved (allow small tolerance)
	const tolerance = time.Second
	if timestamps.ModTime.Sub(testTime).Abs() > tolerance {
		t.Errorf("Modification time not extracted correctly: expected %v, got %v", 
			testTime, timestamps.ModTime)
	}
	
	// Access time should be set (may default to mtime)
	if timestamps.ATime.IsZero() {
		t.Error("Access time should be set")
	}
}

// TestTimestampSetting tests the setFileTimestamps function
func TestTimestampSetting(t *testing.T) {
	tempDir := os.TempDir()
	testFile := filepath.Join(tempDir, "timestamp_set_test.txt")
	
	// Create test file
	f, err := os.Create(testFile)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	f.WriteString("test content")
	f.Close()
	defer os.Remove(testFile)
	
	// Define target timestamps
	targetTime := time.Date(2022, 6, 10, 14, 25, 30, 0, time.UTC)
	targetTimestamps := TimestampInfo{
		ModTime: targetTime,
		ATime:   targetTime,
	}
	
	// Set timestamps
	err = setFileTimestamps(testFile, targetTimestamps)
	if err != nil {
		t.Fatalf("Failed to set timestamps: %v", err)
	}
	
	// Verify timestamps were set correctly
	actualTimestamps, err := getFileTimestamps(testFile)
	if err != nil {
		t.Fatalf("Failed to read back timestamps: %v", err)
	}
	
	const tolerance = time.Second
	if actualTimestamps.ModTime.Sub(targetTime).Abs() > tolerance {
		t.Errorf("Modification time not set correctly: expected %v, got %v", 
			targetTime, actualTimestamps.ModTime)
	}
}

// TestCopyFileWithTimestamps tests the main timestamp preservation functionality
func TestCopyFileWithTimestamps(t *testing.T) {
	tempDir := os.TempDir()
	srcFile := filepath.Join(tempDir, "src_timestamp_test.txt")
	dstFile := filepath.Join(tempDir, "dst_timestamp_test.txt")
	
	// Create source file with specific content and timestamp
	content := "This is test content for timestamp preservation during file copy operations."
	f, err := os.Create(srcFile)
	if err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}
	f.WriteString(content)
	f.Close()
	defer os.Remove(srcFile)
	defer os.Remove(dstFile)
	
	// Set specific timestamp on source file
	originalTime := time.Date(2021, 12, 25, 10, 15, 30, 0, time.UTC)
	err = os.Chtimes(srcFile, originalTime, originalTime)
	if err != nil {
		t.Fatalf("Failed to set source file timestamp: %v", err)
	}
	
	// Get original timestamps
	originalTimestamps, err := getFileTimestamps(srcFile)
	if err != nil {
		t.Fatalf("Failed to get original timestamps: %v", err)
	}
	
	// Copy file with timestamp preservation
	ctx := context.Background()
	err = copyFileWithTimestamps(ctx, srcFile, dstFile)
	if err != nil {
		t.Fatalf("Failed to copy file with timestamps: %v", err)
	}
	
	// Verify destination file exists
	if _, err := os.Stat(dstFile); os.IsNotExist(err) {
		t.Fatal("Destination file was not created")
	}
	
	// Verify content was copied correctly
	dstContent, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}
	if string(dstContent) != content {
		t.Errorf("File content not copied correctly: expected %q, got %q", 
			content, string(dstContent))
	}
	
	// Verify timestamps were preserved
	dstTimestamps, err := getFileTimestamps(dstFile)
	if err != nil {
		t.Fatalf("Failed to get destination timestamps: %v", err)
	}
	
	const tolerance = time.Second
	if dstTimestamps.ModTime.Sub(originalTimestamps.ModTime).Abs() > tolerance {
		t.Errorf("Modification time not preserved: expected %v, got %v", 
			originalTimestamps.ModTime, dstTimestamps.ModTime)
	}
}

// TestCopyFileWithTimestampsContextCancellation tests context cancellation behavior
func TestCopyFileWithTimestampsContextCancellation(t *testing.T) {
	tempDir := os.TempDir()
	srcFile := filepath.Join(tempDir, "src_cancel_test.txt")
	dstFile := filepath.Join(tempDir, "dst_cancel_test.txt")
	
	// Create a larger source file to allow cancellation during copy
	f, err := os.Create(srcFile)
	if err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}
	// Write 1MB of data to ensure copy takes some time
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	f.Write(data)
	f.Close()
	defer os.Remove(srcFile)
	defer os.Remove(dstFile)
	defer os.Remove(dstFile + ".tmp") // Clean up potential temp file
	
	// Create context that cancels immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately
	
	// Attempt copy with cancelled context
	err = copyFileWithTimestamps(ctx, srcFile, dstFile)
	if err == nil {
		t.Error("Expected copy to fail with cancelled context")
	}
	
	// Verify no destination file was created
	if _, err := os.Stat(dstFile); !os.IsNotExist(err) {
		t.Error("Destination file should not exist after cancelled copy")
	}
	
	// Verify no temp file was left behind
	if _, err := os.Stat(dstFile + ".tmp"); !os.IsNotExist(err) {
		t.Error("Temporary file should be cleaned up after cancelled copy")
	}
}

// TestCopyFileWithTimestampsMissingSource tests error handling for missing source files
func TestCopyFileWithTimestampsMissingSource(t *testing.T) {
	tempDir := os.TempDir()
	nonExistentSrc := filepath.Join(tempDir, "nonexistent_file.txt")
	dstFile := filepath.Join(tempDir, "dst_missing_test.txt")
	defer os.Remove(dstFile)
	
	ctx := context.Background()
	err := copyFileWithTimestamps(ctx, nonExistentSrc, dstFile)
	if err == nil {
		t.Error("Expected copy to fail with missing source file")
	}
	
	// Verify no destination file was created
	if _, err := os.Stat(dstFile); !os.IsNotExist(err) {
		t.Error("Destination file should not exist after failed copy")
	}
}

// BenchmarkCopyFileWithTimestamps benchmarks the timestamp-preserving copy
func BenchmarkCopyFileWithTimestamps(b *testing.B) {
	tempDir := os.TempDir()
	srcFile := filepath.Join(tempDir, "bench_src.txt")
	
	// Create source file with 1KB of data
	content := make([]byte, 1024)
	for i := range content {
		content[i] = byte(i % 256)
	}
	
	err := os.WriteFile(srcFile, content, 0644)
	if err != nil {
		b.Fatalf("Failed to create source file: %v", err)
	}
	defer os.Remove(srcFile)
	
	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dstFile := filepath.Join(tempDir, fmt.Sprintf("bench_dst_%d.txt", i))
		err := copyFileWithTimestamps(ctx, srcFile, dstFile)
		if err != nil {
			b.Fatalf("Copy failed: %v", err)
		}
		os.Remove(dstFile) // Clean up for next iteration
	}
}

// TestTimestampVerification tests the timestamp verification logic
func TestTimestampVerification(t *testing.T) {
	tempDir := os.TempDir()
	testFile := filepath.Join(tempDir, "verify_test.txt")
	
	// Create test file
	f, err := os.Create(testFile)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	f.WriteString("test")
	f.Close()
	defer os.Remove(testFile)
	
	// Get current timestamps
	currentTimestamps, err := getFileTimestamps(testFile)
	if err != nil {
		t.Fatalf("Failed to get timestamps: %v", err)
	}
	
	// Verification should pass with same timestamps
	err = verifyTimestamps(testFile, currentTimestamps)
	if err != nil {
		t.Errorf("Verification should pass with matching timestamps: %v", err)
	}
	
	// Verification should fail with very different timestamps
	oldTimestamps := TimestampInfo{
		ModTime: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		ATime:   time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	
	err = verifyTimestamps(testFile, oldTimestamps)
	if err == nil {
		t.Error("Verification should fail with very different timestamps")
	}
}