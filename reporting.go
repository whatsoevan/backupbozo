// bozobackup: Incremental, deduplicating photo/video backup tool with HTML reporting.
package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"html"
)

// SkippedFile type is defined in pipeline.go with other processing structures

// writeHTMLReport generates a detailed HTML report of the backup session
// Includes summary stats, clickable file links, skipped reasons, and errors
func writeHTMLReport(path string, summary AccountingSummary, totalTime time.Duration) {
	f, err := os.Create(path)
	if err != nil {
		log.Printf("Could not create report: %v", err)
		return
	}
	defer f.Close()
	f.WriteString("<html><head><title>bozobackup report</title></head><body>")
	f.WriteString("<h1>bozobackup Report</h1>")
	f.WriteString("<h2>Summary</h2><ul>")
	f.WriteString(fmt.Sprintf("<li>Total copied size: %.2f MB</li>", float64(summary.TotalBytes)/(1024*1024)))
	f.WriteString(fmt.Sprintf("<li>Total time taken: %s</li>", totalTime))
	f.WriteString(fmt.Sprintf("<li>Files copied: %d</li>", summary.Copied))
	f.WriteString(fmt.Sprintf("<li>Duplicates skipped: %d</li>", summary.Duplicates))
	f.WriteString(fmt.Sprintf("<li>Files skipped: %d</li>", summary.Skipped))
	f.WriteString(fmt.Sprintf("<li>Errors: %d</li>", summary.Errors))
	f.WriteString("</ul>")
	f.WriteString("<h2>Copied Files</h2><ul>")
	for _, pair := range summary.CopiedFiles {
		srcAbs := html.EscapeString(pair[0])
		dstAbs := html.EscapeString(pair[1])
		f.WriteString(fmt.Sprintf("<li><a href=\"file://%s\">%s</a> → <a href=\"file://%s\">%s</a></li>", srcAbs, srcAbs, dstAbs, dstAbs))
	}
	f.WriteString("</ul>")
	if len(summary.DuplicateFiles) > 0 {
		f.WriteString("<h2>Skipped Duplicates</h2><ul>")
		for _, pair := range summary.DuplicateFiles {
			srcAbs := html.EscapeString(pair[0])
			dstAbs := html.EscapeString(pair[1])
			f.WriteString(fmt.Sprintf("<li><a href=\"file://%s\">%s</a> (would copy to <a href=\"file://%s\">%s</a>)</li>", srcAbs, srcAbs, dstAbs, dstAbs))
		}
		f.WriteString("</ul>")
	}
	if len(summary.SkippedFiles) > 0 {
		f.WriteString("<h2>Skipped Files</h2><ul>")
		for _, s := range summary.SkippedFiles {
			absPath := html.EscapeString(s.Path)
			f.WriteString(fmt.Sprintf("<li><a href=\"file://%s\">%s</a> — %s</li>", absPath, absPath, html.EscapeString(s.Reason)))
		}
		f.WriteString("</ul>")
	}
	if len(summary.ErrorList) > 0 {
		f.WriteString("<h2>Errors</h2><ul>")
		for _, e := range summary.ErrorList {
			f.WriteString(fmt.Sprintf("<li>%s</li>", html.EscapeString(e)))
		}
		f.WriteString("</ul>")
	}
	f.WriteString("</body></html>")
}
