// bozobackup: Incremental, deduplicating photo/video backup tool with HTML reporting.
package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"html"
)

// SkippedFile records a file that was skipped and the reason
// (for HTML reporting and transparency)
type SkippedFile struct {
	Path   string // Absolute file path
	Reason string // Reason for skipping
}

// writeHTMLReport generates a detailed HTML report of the backup session
// Includes summary stats, clickable file links, skipped reasons, and errors
func writeHTMLReport(path string, copiedFiles, duplicateFiles [][2]string, skippedFiles []SkippedFile, errors []string, totalCopiedSize int64, totalTime time.Duration) {
	f, err := os.Create(path)
	if err != nil {
		log.Printf("Could not create report: %v", err)
		return
	}
	defer f.Close()
	f.WriteString("<html><head><title>bozobackup report</title></head><body>")
	f.WriteString("<h1>bozobackup Report</h1>")
	f.WriteString("<h2>Summary</h2><ul>")
	f.WriteString(fmt.Sprintf("<li>Total copied size: %.2f MB</li>", float64(totalCopiedSize)/(1024*1024)))
	f.WriteString(fmt.Sprintf("<li>Total time taken: %s</li>", totalTime))
	f.WriteString(fmt.Sprintf("<li>Files copied: %d</li>", len(copiedFiles)))
	f.WriteString(fmt.Sprintf("<li>Duplicates skipped: %d</li>", len(duplicateFiles)))
	f.WriteString(fmt.Sprintf("<li>Files skipped: %d</li>", len(skippedFiles)))
	f.WriteString(fmt.Sprintf("<li>Errors: %d</li>", len(errors)))
	f.WriteString("</ul>")
	f.WriteString("<h2>Copied Files</h2><ul>")
	for _, pair := range copiedFiles {
		srcAbs := html.EscapeString(pair[0])
		dstAbs := html.EscapeString(pair[1])
		f.WriteString(fmt.Sprintf("<li><a href=\"file://%s\">%s</a> → <a href=\"file://%s\">%s</a></li>", srcAbs, srcAbs, dstAbs, dstAbs))
	}
	f.WriteString("</ul>")
	if len(duplicateFiles) > 0 {
		f.WriteString("<h2>Skipped Duplicates</h2><ul>")
		for _, pair := range duplicateFiles {
			srcAbs := html.EscapeString(pair[0])
			dstAbs := html.EscapeString(pair[1])
			f.WriteString(fmt.Sprintf("<li><a href=\"file://%s\">%s</a> (would copy to <a href=\"file://%s\">%s</a>)</li>", srcAbs, srcAbs, dstAbs, dstAbs))
		}
		f.WriteString("</ul>")
	}
	if len(skippedFiles) > 0 {
		f.WriteString("<h2>Skipped Files</h2><ul>")
		for _, s := range skippedFiles {
			absPath := html.EscapeString(s.Path)
			f.WriteString(fmt.Sprintf("<li><a href=\"file://%s\">%s</a> — %s</li>", absPath, absPath, html.EscapeString(s.Reason)))
		}
		f.WriteString("</ul>")
	}
	if len(errors) > 0 {
		f.WriteString("<h2>Errors</h2><ul>")
		for _, e := range errors {
			f.WriteString(fmt.Sprintf("<li>%s</li>", html.EscapeString(e)))
		}
		f.WriteString("</ul>")
	}
	f.WriteString("</body></html>")
}
