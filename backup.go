// bozobackup: Incremental, deduplicating photo/video backup tool with HTML reporting.
package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/fatih/color"
	"github.com/schollz/progressbar/v3"
)

// backup is the main backup routine: scans, checks, copies, and reports
// Now supports context cancellation for safe Ctrl+C handling
func backup(ctx context.Context, srcDir, destDir, dbPath, reportPath string, incremental bool) {
	checkDirExists(srcDir, "Source")
	checkDirExists(destDir, "Destination")

	db := initDB(dbPath)
	defer db.Close()

	startTime := time.Now()

	var minMtime int64 = 0
	var lastBackupTime time.Time
	if incremental {
		var err error
		lastBackupTime, err = getLastBackupTime(db)
		if err == nil && !lastBackupTime.IsZero() {
			minMtime = lastBackupTime.Unix()
		}
	} else {
		// info: incremental mode disabled (removed print)
	}

	// Scan all files in source directory
	files, walkErrors := getAllFiles(srcDir)

	// Single-pass processing with FileCandidate caching
	var copied, duplicates, errors int
	var errorList []string
	var copiedFiles [][2]string    // [][src, dst] for HTML report
	var duplicateFiles [][2]string // [][src, dst] for HTML report
	var skippedFiles []SkippedFile // Skipped files and reasons for HTML report
	var totalCopiedSize int64
	var estimatedTotalSize int64

	// Create progress bar for single-pass processing
	bar := progressbar.NewOptions(
		len(files),
		progressbar.OptionSetDescription("Processing files"),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetWidth(50),
		progressbar.OptionSetPredictTime(true), // ETA
		progressbar.OptionSetElapsedTime(true), // Elapsed
		progressbar.OptionClearOnFinish(),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSpinnerType(14), // Use a spinner
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)

	// Single pass: evaluate and process each file
	for _, file := range files {
		select {
		case <-ctx.Done():
			color.New(color.FgRed, color.Bold).Println("Backup interrupted by user. Writing partial report and exiting.")
			goto cleanup
		default:
		}
		if ctx.Err() != nil {
			goto cleanup
		}
		bar.Add(1)

		// Create FileCandidate (caches os.Stat, extension, etc.)
		candidate, err := NewFileCandidate(file, destDir)
		if err != nil {
			errorList = append(errorList, fmt.Sprintf("%s: candidate creation error: %v", file, err))
			errors++
			continue
		}

		// Single evaluation using cached data (replaces duplicate logic)
		decision := evaluateFileForBackup(candidate, db, incremental, minMtime)

		// Handle the decision
		switch decision.State {
		case StateSkippedExtension, StateSkippedIncremental, StateSkippedDate, StateSkippedDestExists:
			skippedFiles = append(skippedFiles, SkippedFile{
				Path:   candidate.Path,
				Reason: decision.Reason,
			})

		case StateDuplicateHash:
			duplicates++
			duplicateFiles = append(duplicateFiles, [2]string{candidate.Path, candidate.DestPath})

		case StateErrorStat, StateErrorDate, StateErrorHash:
			errorList = append(errorList, fmt.Sprintf("%s: %s", candidate.Path, decision.Reason))
			errors++

		case StateCopied:
			// Actually copy the file
			if err := copyFileWithTimestamps(ctx, candidate.Path, candidate.DestPath); err != nil {
				errorList = append(errorList, fmt.Sprintf("%s: copy error: %v", candidate.Path, err))
				errors++
				if ctx.Err() != nil {
					break
				}
				continue
			}

			// Record successful copy
			insertFileRecord(db, candidate.Path, candidate.DestPath, candidate.Hash, 
						   candidate.Info.Size(), candidate.Info.ModTime().Unix())
			copied++
			copiedFiles = append(copiedFiles, [2]string{candidate.Path, candidate.DestPath})
			totalCopiedSize += candidate.Info.Size()
		}

		// Track estimated size for space checking (done incrementally now)
		if decision.ShouldCopy {
			estimatedTotalSize += decision.EstimatedSize
		}
	}

	// Note: Free space checking is now done incrementally during processing
	// This could be enhanced to check periodically and stop early if space runs out

cleanup:
	// Log any errors from walking the file tree
	for _, walkErr := range walkErrors {
		errorList = append(errorList, fmt.Sprintf("walk error: %v", walkErr))
	}

	totalTime := time.Since(startTime)

	// Generate HTML report with all results
	writeHTMLReport(reportPath, copiedFiles, duplicateFiles, skippedFiles, errorList, totalCopiedSize, totalTime)

	// Print a summary and check accounting
	totalFound := len(files)
	totalCopied := len(copiedFiles)
	totalSkipped := len(skippedFiles)
	totalDuplicates := len(duplicateFiles)
	totalErrors := errors + len(walkErrors)

	// Calculate unaccounted files by checking what's missing
	unaccountedFiles := totalFound - totalCopied - totalSkipped - totalDuplicates - totalErrors

	// If there are unaccounted files, they were likely skipped in the second pass
	// but not properly tracked. Add them to the skipped count for accounting purposes.
	if unaccountedFiles > 0 {
		totalSkipped += unaccountedFiles
	}

	totalAccounted := totalCopied + totalSkipped + totalDuplicates + totalErrors

	fmt.Println()
	color.New(color.FgGreen).Printf("Copied: %d, ", totalCopied)
	color.New(color.FgYellow).Printf("Skipped: %d, Duplicates: %d, ", totalSkipped, totalDuplicates)
	color.New(color.FgRed).Printf("Errors: %d, ", totalErrors)
	fmt.Printf("Total Found: %d\n", totalFound)
	if totalAccounted == totalFound {
		color.New(color.FgGreen, color.Bold).Println("✔ All files accounted for!")
	} else {
		color.New(color.FgRed, color.Bold).Printf("✖ Mismatch! Accounted: %d, Found: %d\n", totalAccounted, totalFound)
	}
	// Print clickable link to HTML report (file://...)
	reportAbs, err := filepath.Abs(reportPath)
	if err == nil {
		link := fmt.Sprintf("file://%s", reportAbs)
		// ANSI hyperlink: \x1b]8;;<url>\x1b\\<text>\x1b]8;;\x1b\\
		ansiLink := fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", link, link)
		color.New(color.FgCyan).Printf("HTML report: %s\n", ansiLink)
	} else {
		color.New(color.FgCyan).Printf("HTML report: %s\n", reportPath)
	}
}
