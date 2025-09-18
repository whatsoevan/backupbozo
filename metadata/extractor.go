// Package metadata provides comprehensive date and metadata extraction for media files
package metadata

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
)

// MetadataResult contains extracted metadata with confidence level and source information
type MetadataResult struct {
	Date       time.Time     // Best extracted date
	Confidence Confidence    // How reliable this date is
	Source     string        // Where the date came from (e.g., "EXIF DateTimeOriginal")
	Error      error         // Any error during extraction
	Duration   time.Duration // Time taken to extract (for performance monitoring)
}

// Confidence represents how reliable the extracted date is
type Confidence int

const (
	ConfidenceNone Confidence = iota // No date found or extraction failed
	ConfidenceLow                    // Filesystem mtime or unreliable metadata
	ConfidenceMedium                 // Some metadata but limited reliability (PNG, AVI)
	ConfidenceHigh                   // Reliable camera/device metadata (EXIF, video creation_time)
)

func (c Confidence) String() string {
	switch c {
	case ConfidenceNone:
		return "none"
	case ConfidenceLow:
		return "low"
	case ConfidenceMedium:
		return "medium"
	case ConfidenceHigh:
		return "high"
	default:
		return "unknown"
	}
}

// MetadataExtractor defines the interface for extracting metadata from files
type MetadataExtractor interface {
	// CanHandle returns true if this extractor can process the given file extension
	CanHandle(extension string) bool
	
	// ExtractDate extracts the best available date from the file
	ExtractDate(path string) MetadataResult
	
	// Name returns the name of this extractor for logging/debugging
	Name() string
}

// ExtractorRegistry manages multiple metadata extractors
type ExtractorRegistry struct {
	extractors []MetadataExtractor
}

// NewExtractorRegistry creates a registry with all available extractors
func NewExtractorRegistry() *ExtractorRegistry {
	return &ExtractorRegistry{
		extractors: []MetadataExtractor{
			&EXIFExtractor{},
			&VideoExtractor{},
			&PNGExtractor{},
			&FilesystemExtractor{}, // Always last as fallback
		},
	}
}

// ExtractBestDate tries all extractors and returns the best date found
func (r *ExtractorRegistry) ExtractBestDate(path string) MetadataResult {
	ext := strings.ToLower(filepath.Ext(path))
	
	var bestResult MetadataResult
	bestResult.Confidence = ConfidenceNone
	
	start := time.Now()
	defer func() {
		if bestResult.Duration == 0 {
			bestResult.Duration = time.Since(start)
		}
	}()
	
	// Try each extractor that can handle this file type
	for _, extractor := range r.extractors {
		if !extractor.CanHandle(ext) {
			continue
		}
		
		result := extractor.ExtractDate(path)
		
		// Use this result if it's better than what we have
		if result.Confidence > bestResult.Confidence || 
		   (result.Confidence == bestResult.Confidence && result.Error == nil && bestResult.Error != nil) {
			bestResult = result
		}
		
		// If we got high confidence, we can stop looking
		if bestResult.Confidence == ConfidenceHigh && bestResult.Error == nil {
			break
		}
	}
	
	bestResult.Duration = time.Since(start)
	return bestResult
}

// EXIFExtractor handles JPEG and HEIC files with comprehensive EXIF date extraction
type EXIFExtractor struct{}

func (e *EXIFExtractor) Name() string {
	return "EXIF"
}

func (e *EXIFExtractor) CanHandle(extension string) bool {
	switch extension {
	case ".jpg", ".jpeg", ".heic", ".heif":
		return true
	default:
		return false
	}
}

func (e *EXIFExtractor) ExtractDate(path string) MetadataResult {
	start := time.Now()
	
	f, err := os.Open(path)
	if err != nil {
		return MetadataResult{
			Confidence: ConfidenceNone,
			Source:     "EXIF",
			Error:      fmt.Errorf("failed to open file: %w", err),
			Duration:   time.Since(start),
		}
	}
	defer f.Close()
	
	// Decode EXIF data
	x, err := exif.Decode(f)
	if err != nil {
		return MetadataResult{
			Confidence: ConfidenceNone,
			Source:     "EXIF",
			Error:      fmt.Errorf("failed to decode EXIF: %w", err),
			Duration:   time.Since(start),
		}
	}
	
	// Try EXIF date fields in order of preference (most reliable first)
	dateFields := []struct {
		field  exif.FieldName
		source string
	}{
		{exif.DateTimeOriginal, "EXIF DateTimeOriginal"},     // Best: when photo was taken
		{exif.DateTimeDigitized, "EXIF DateTimeDigitized"},   // Good: when photo was digitized
		{exif.DateTime, "EXIF DateTime"},                     // OK: when file was last modified
	}
	
	for _, field := range dateFields {
		if tag, err := x.Get(field.field); err == nil {
			if dateStr, err := tag.StringVal(); err == nil {
				// Parse EXIF date format: "2006:01:02 15:04:05"
				if date, err := time.Parse("2006:01:02 15:04:05", dateStr); err == nil {
					return MetadataResult{
						Date:       date,
						Confidence: ConfidenceHigh,
						Source:     field.source,
						Duration:   time.Since(start),
					}
				}
			}
		}
	}
	
	// Try the legacy DateTime() method as fallback
	if dt, err := x.DateTime(); err == nil {
		return MetadataResult{
			Date:       dt,
			Confidence: ConfidenceHigh,
			Source:     "EXIF DateTime (legacy)",
			Duration:   time.Since(start),
		}
	}
	
	return MetadataResult{
		Confidence: ConfidenceNone,
		Source:     "EXIF",
		Error:      fmt.Errorf("no valid date fields found in EXIF"),
		Duration:   time.Since(start),
	}
}

// VideoExtractor handles video files using ffprobe with multiple fallback strategies
type VideoExtractor struct{}

func (v *VideoExtractor) Name() string {
	return "Video"
}

func (v *VideoExtractor) CanHandle(extension string) bool {
	switch extension {
	case ".mp4", ".mov", ".mkv", ".webm", ".avi":
		return true
	default:
		return false
	}
}

func (v *VideoExtractor) ExtractDate(path string) MetadataResult {
	start := time.Now()
	
	// Use ffprobe to extract all metadata (not just format)
	cmd := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", path)
	out, err := cmd.Output()
	if err != nil {
		return MetadataResult{
			Confidence: ConfidenceNone,
			Source:     "ffprobe",
			Error:      fmt.Errorf("ffprobe failed: %w", err),
			Duration:   time.Since(start),
		}
	}
	
	// Parse ffprobe output
	var data struct {
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
		Streams []struct {
			Tags map[string]string `json:"tags"`
		} `json:"streams"`
	}
	
	if err := json.Unmarshal(out, &data); err != nil {
		return MetadataResult{
			Confidence: ConfidenceNone,
			Source:     "ffprobe",
			Error:      fmt.Errorf("failed to parse ffprobe output: %w", err),
			Duration:   time.Since(start),
		}
	}
	
	// Try multiple date fields in order of preference
	dateFields := []struct {
		source string
		getter func() string
	}{
		// Format-level tags (most common)
		{"creation_time", func() string { return data.Format.Tags["creation_time"] }},
		{"date", func() string { return data.Format.Tags["date"] }},
		
		// Apple/QuickTime specific
		{"com.apple.quicktime.creationdate", func() string { return data.Format.Tags["com.apple.quicktime.creationdate"] }},
		
		// Stream-level creation time (fallback)
		{"stream creation_time", func() string {
			for _, stream := range data.Streams {
				if ct := stream.Tags["creation_time"]; ct != "" {
					return ct
				}
			}
			return ""
		}},
	}
	
	for _, field := range dateFields {
		dateStr := field.getter()
		if dateStr == "" {
			continue
		}
		
		// Try parsing different date formats
		formats := []string{
			time.RFC3339,                    // 2006-01-02T15:04:05Z07:00
			"2006-01-02T15:04:05",          // Without timezone
			"2006-01-02 15:04:05",          // Space separated
			"2006:01:02 15:04:05",          // EXIF-like format
		}
		
		for _, format := range formats {
			if date, err := time.Parse(format, dateStr); err == nil {
				confidence := ConfidenceHigh
				// Lower confidence for some container formats
				ext := strings.ToLower(filepath.Ext(path))
				if ext == ".avi" || ext == ".webm" {
					confidence = ConfidenceMedium
				}
				
				return MetadataResult{
					Date:       date,
					Confidence: confidence,
					Source:     fmt.Sprintf("Video %s", field.source),
					Duration:   time.Since(start),
				}
			}
		}
	}
	
	return MetadataResult{
		Confidence: ConfidenceNone,
		Source:     "ffprobe",
		Error:      fmt.Errorf("no valid creation time found in video metadata"),
		Duration:   time.Since(start),
	}
}

// PNGExtractor handles PNG files (limited metadata support)
type PNGExtractor struct{}

func (p *PNGExtractor) Name() string {
	return "PNG"
}

func (p *PNGExtractor) CanHandle(extension string) bool {
	return extension == ".png"
}

func (p *PNGExtractor) ExtractDate(path string) MetadataResult {
	start := time.Now()
	
	// PNG files rarely have reliable creation date metadata
	// Most PNGs are screenshots, edited images, or generated content
	// We'll still try to extract any available text chunks that might contain dates
	
	f, err := os.Open(path)
	if err != nil {
		return MetadataResult{
			Confidence: ConfidenceNone,
			Source:     "PNG",
			Error:      fmt.Errorf("failed to open PNG file: %w", err),
			Duration:   time.Since(start),
		}
	}
	defer f.Close()
	
	// For now, PNG extraction is minimal since most PNGs don't have reliable dates
	// This is a placeholder for future enhancement with PNG chunk parsing
	
	return MetadataResult{
		Confidence: ConfidenceNone,
		Source:     "PNG",
		Error:      fmt.Errorf("PNG date extraction not implemented (PNGs rarely have reliable creation dates)"),
		Duration:   time.Since(start),
	}
}

// FilesystemExtractor provides filesystem modification time as fallback
type FilesystemExtractor struct{}

func (f *FilesystemExtractor) Name() string {
	return "Filesystem"
}

func (f *FilesystemExtractor) CanHandle(extension string) bool {
	return true // Can handle any file as fallback
}

func (f *FilesystemExtractor) ExtractDate(path string) MetadataResult {
	start := time.Now()
	
	info, err := os.Stat(path)
	if err != nil {
		return MetadataResult{
			Confidence: ConfidenceNone,
			Source:     "Filesystem",
			Error:      fmt.Errorf("failed to stat file: %w", err),
			Duration:   time.Since(start),
		}
	}
	
	return MetadataResult{
		Date:       info.ModTime(),
		Confidence: ConfidenceLow,
		Source:     "Filesystem mtime",
		Duration:   time.Since(start),
	}
}