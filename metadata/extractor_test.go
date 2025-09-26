// Package metadata tests for comprehensive date extraction
package metadata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestExtractorRegistry tests the main registry functionality
func TestExtractorRegistry(t *testing.T) {
	registry := NewExtractorRegistry()

	// Test that we have extractors
	if len(registry.extractors) == 0 {
		t.Fatal("Registry should have extractors")
	}

	// Verify we have the expected extractors
	expectedExtractors := []string{"EXIF", "Video", "PNG", "Filesystem"}
	if len(registry.extractors) != len(expectedExtractors) {
		t.Errorf("Expected %d extractors, got %d", len(expectedExtractors), len(registry.extractors))
	}

	for i, extractor := range registry.extractors {
		if extractor.Name() != expectedExtractors[i] {
			t.Errorf("Expected extractor %s at position %d, got %s",
				expectedExtractors[i], i, extractor.Name())
		}
	}
}

// TestEXIFExtractorCanHandle tests EXIF extractor file type support
func TestEXIFExtractorCanHandle(t *testing.T) {
	extractor := &EXIFExtractor{}

	testCases := []struct {
		extension string
		expected  bool
	}{
		{".jpg", true},
		{".jpeg", true},
		{".heic", true}, // Critical: HEIC support for iPhone photos
		{".heif", true}, // HEIF support
		{".png", false},
		{".mp4", false},
		{".txt", false},
	}

	for _, tc := range testCases {
		result := extractor.CanHandle(tc.extension)
		if result != tc.expected {
			t.Errorf("EXIF extractor CanHandle(%s) = %v, expected %v",
				tc.extension, result, tc.expected)
		}
	}
}

// TestVideoExtractorCanHandle tests video extractor file type support
func TestVideoExtractorCanHandle(t *testing.T) {
	extractor := &VideoExtractor{}

	testCases := []struct {
		extension string
		expected  bool
	}{
		{".mp4", true},
		{".mov", true},
		{".mkv", true},
		{".webm", true},
		{".avi", true},
		{".jpg", false},
		{".heic", false},
		{".png", false},
	}

	for _, tc := range testCases {
		result := extractor.CanHandle(tc.extension)
		if result != tc.expected {
			t.Errorf("Video extractor CanHandle(%s) = %v, expected %v",
				tc.extension, result, tc.expected)
		}
	}
}

// TestPNGExtractorCanHandle tests PNG extractor file type support
func TestPNGExtractorCanHandle(t *testing.T) {
	extractor := &PNGExtractor{}

	testCases := []struct {
		extension string
		expected  bool
	}{
		{".png", true},
		{".jpg", false},
		{".heic", false},
		{".mp4", false},
	}

	for _, tc := range testCases {
		result := extractor.CanHandle(tc.extension)
		if result != tc.expected {
			t.Errorf("PNG extractor CanHandle(%s) = %v, expected %v",
				tc.extension, result, tc.expected)
		}
	}
}

// TestFilesystemExtractor tests filesystem fallback extractor
func TestFilesystemExtractor(t *testing.T) {
	extractor := &FilesystemExtractor{}

	// Should handle any extension
	extensions := []string{".jpg", ".heic", ".mp4", ".png", ".txt", ""}
	for _, ext := range extensions {
		if !extractor.CanHandle(ext) {
			t.Errorf("Filesystem extractor should handle any extension, failed for %s", ext)
		}
	}

	// Test with a real file
	tempDir := os.TempDir()
	testFile := filepath.Join(tempDir, "test_filesystem.txt")

	// Create test file with known modification time
	testTime := time.Date(2023, 6, 15, 10, 30, 45, 0, time.UTC)
	err := os.WriteFile(testFile, []byte("test content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)

	// Set specific modification time
	err = os.Chtimes(testFile, testTime, testTime)
	if err != nil {
		t.Fatalf("Failed to set file times: %v", err)
	}

	// Extract date
	result := extractor.ExtractDate(testFile)

	// Verify result
	if result.Error != nil {
		t.Errorf("Filesystem extraction should not error: %v", result.Error)
	}

	if result.Confidence != ConfidenceLow {
		t.Errorf("Filesystem extraction should have low confidence, got %v", result.Confidence)
	}

	if result.Source != "Filesystem mtime" {
		t.Errorf("Expected source 'Filesystem mtime', got %s", result.Source)
	}

	// Check date is close (within 1 second tolerance)
	if result.Date.Sub(testTime).Abs() > time.Second {
		t.Errorf("Expected date close to %v, got %v", testTime, result.Date)
	}
}

// TestExtractorRegistryWithFileSystemFallback tests that filesystem fallback works
func TestExtractorRegistryWithFilesystemFallback(t *testing.T) {
	registry := NewExtractorRegistry()

	tempDir := os.TempDir()
	testFile := filepath.Join(tempDir, "test_fallback.unknown")

	testTime := time.Date(2023, 4, 20, 14, 15, 30, 0, time.UTC)
	err := os.WriteFile(testFile, []byte("unknown file type"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)

	err = os.Chtimes(testFile, testTime, testTime)
	if err != nil {
		t.Fatalf("Failed to set file times: %v", err)
	}

	// Extract using registry
	result := registry.ExtractBestDate(testFile)

	// Should fallback to filesystem
	if result.Error != nil {
		t.Errorf("Registry extraction should not error for unknown file: %v", result.Error)
	}

	if result.Confidence != ConfidenceLow {
		t.Errorf("Unknown file should have low confidence, got %v", result.Confidence)
	}

	if !strings.Contains(result.Source, "Filesystem") {
		t.Errorf("Expected filesystem source, got %s", result.Source)
	}
}

// TestConfidenceString tests confidence level string representation
func TestConfidenceString(t *testing.T) {
	testCases := []struct {
		confidence Confidence
		expected   string
	}{
		{ConfidenceNone, "none"},
		{ConfidenceLow, "low"},
		{ConfidenceMedium, "medium"},
		{ConfidenceHigh, "high"},
	}

	for _, tc := range testCases {
		result := tc.confidence.String()
		if result != tc.expected {
			t.Errorf("Confidence %v should stringify to %s, got %s",
				tc.confidence, tc.expected, result)
		}
	}
}

// TestMetadataResultDuration tests that duration is measured
func TestMetadataResultDuration(t *testing.T) {
	extractor := &FilesystemExtractor{}

	tempDir := os.TempDir()
	testFile := filepath.Join(tempDir, "test_duration.txt")

	err := os.WriteFile(testFile, []byte("test"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)

	result := extractor.ExtractDate(testFile)

	if result.Duration <= 0 {
		t.Error("Duration should be positive")
	}

	if result.Duration > time.Second {
		t.Error("Duration should be very short for filesystem extraction")
	}
}

// TestExtractorRegistryPerformance benchmarks the extraction performance
func BenchmarkExtractorRegistry(b *testing.B) {
	registry := NewExtractorRegistry()

	tempDir := os.TempDir()
	testFile := filepath.Join(tempDir, "bench_test.jpg")

	// Create a fake JPEG file (will fail EXIF but test the pipeline)
	err := os.WriteFile(testFile, []byte("fake jpeg content"), 0644)
	if err != nil {
		b.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := registry.ExtractBestDate(testFile)
		_ = result // Use result to prevent optimization
	}
}

// TestEXIFExtractorWithInvalidFile tests EXIF extractor error handling
func TestEXIFExtractorWithInvalidFile(t *testing.T) {
	extractor := &EXIFExtractor{}

	tempDir := os.TempDir()
	testFile := filepath.Join(tempDir, "fake.jpg")

	// Create fake JPEG file that will fail EXIF parsing
	err := os.WriteFile(testFile, []byte("not a real jpeg"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)

	result := extractor.ExtractDate(testFile)

	// Should handle error gracefully
	if result.Error == nil {
		t.Error("Expected error for invalid JPEG file")
	}

	if result.Confidence != ConfidenceNone {
		t.Errorf("Expected no confidence for invalid file, got %v", result.Confidence)
	}

	if result.Duration <= 0 {
		t.Error("Duration should still be measured even on error")
	}
}

// TestVideoExtractorWithoutFFprobe tests video extractor when ffprobe is not available
func TestVideoExtractorWithoutFFprobe(t *testing.T) {
	extractor := &VideoExtractor{}

	tempDir := os.TempDir()
	testFile := filepath.Join(tempDir, "fake.mp4")

	err := os.WriteFile(testFile, []byte("not a real video"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer os.Remove(testFile)

	result := extractor.ExtractDate(testFile)

	// Should handle missing ffprobe or invalid video gracefully
	// (ffprobe might not be available in test environment)
	if result.Confidence == ConfidenceHigh {
		t.Error("Should not have high confidence for fake video file")
	}

	if result.Duration <= 0 {
		t.Error("Duration should be measured even on error")
	}
}
