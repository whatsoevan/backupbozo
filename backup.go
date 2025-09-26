// bozobackup: Incremental, deduplicating photo/video backup tool with HTML reporting.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/schollz/progressbar/v3"
)

// getFreeSpace returns available disk space for the given path
func getFreeSpace(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

// checkDirExists validates that a directory exists, exits with error if not
func checkDirExists(path string, label string) {
	info, err := os.Stat(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] %s directory '%s' does not exist: %v\n", label, path, err)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "[FATAL] %s path '%s' is not a directory\n", label, path)
		os.Exit(1)
	}
}

// backup is the main backup routine: scans, checks, copies, and reports
// Now supports context cancellation for safe Ctrl+C handling and parallel processing
func backup(ctx context.Context, srcDir, destDir, dbPath, reportPath string, incremental bool, workers int) {
	checkDirExists(srcDir, "Source")
	checkDirExists(destDir, "Destination")

	db := initDB(dbPath)
	defer db.Close()

	// Load existing hashes into memory for fast duplicate detection
	hashToPath := loadExistingHashes(db)

	// Create batch inserter for efficient database writes
	batchInserter := NewBatchInserter(db, hashToPath, 1000)
	defer func() {
		// Use context-aware flush with a short timeout for cleanup
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer flushCancel()
		batchInserter.FlushWithContext(flushCtx)
	}()

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

	// PHASE 1: Planning phase - fast evaluation without hash computation
	fmt.Println()
	color.New(color.FgCyan, color.Bold).Printf("📋 Planning Phase\n")
	fmt.Printf("   Scanning %d files from source directory...\n", len(files))
	planningBar := progressbar.NewOptions(
		len(files),
		progressbar.OptionSetDescription("Planning"),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetWidth(50),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[blue]=[reset]",
			SaucerHead:    "[blue]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)

	var estimatedTotalSize int64
	var filesToCopy int

	// Fast parallel planning evaluation (no hash computation)
	planningResults := evaluateFilesForPlanningParallel(ctx, files, destDir, planningBar, incremental, minMtime, workers)

	// Check for cancellation after planning
	if ctx.Err() != nil {
		fmt.Printf("\nBackup planning interrupted\n")
		fmt.Printf("No files were processed. Restart to begin backup.\n")
		return
	}

	// Aggregate planning results
	for _, planResult := range planningResults {
		if planResult.ShouldCopy {
			estimatedTotalSize += planResult.Size
			filesToCopy++
		}
	}

	// Check available disk space
	availableSpace, err := getFreeSpace(destDir)
	if err != nil {
		color.New(color.FgRed, color.Bold).Printf("Error checking disk space: %v\n", err)
		return
	}

	// Space check with clear abort/continue decision
	const spaceBuffer = uint64(1024 * 1024 * 100) // 100MB safety buffer
	requiredSpace := uint64(estimatedTotalSize) + spaceBuffer

	fmt.Println()
	color.New(color.FgBlue, color.Bold).Printf("💾 Space Analysis\n")
	color.New(color.FgCyan).Printf("   Files found in source: %d\n", len(files))
	color.New(color.FgYellow).Printf("   Files estimated for copy: %d\n", filesToCopy)
	color.New(color.FgMagenta).Printf("   Estimated copy size: %.2f GB\n", float64(estimatedTotalSize)/(1024*1024*1024))
	color.New(color.FgGreen).Printf("   Available disk space: %.2f GB\n", float64(availableSpace)/(1024*1024*1024))
	color.New(color.FgBlue).Printf("   Required (with buffer): %.2f GB\n", float64(requiredSpace)/(1024*1024*1024))

	if availableSpace < requiredSpace {
		color.New(color.FgRed, color.Bold).Printf("\n❌ INSUFFICIENT DISK SPACE\n")
		fmt.Printf("Need %.2f GB but only %.2f GB available.\n",
			float64(requiredSpace)/(1024*1024*1024),
			float64(availableSpace)/(1024*1024*1024))
		fmt.Printf("Please free up space or use a different destination.\n")
		return
	}

	color.New(color.FgGreen, color.Bold).Printf("   ✅ Sufficient disk space available\n")

	// PHASE 2: Execution phase - actual processing with hash computation and copying
	fmt.Println()
	color.New(color.FgGreen, color.Bold).Printf("🚀 Executing Backup\n")
	fmt.Printf("   Processing %d files with %d workers...\n", len(files), workers)

	execBar := progressbar.NewOptions(
		len(files),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetWidth(50),
		progressbar.OptionSetPredictTime(true), // ETA
		progressbar.OptionSetElapsedTime(true), // Elapsed
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

	// Parallel processing: use worker pool for concurrent file processing
	if workers <= 0 {
		workers = 1 // Fallback to single-threaded if invalid worker count
	}
	results := processFilesParallel(ctx, files, srcDir, destDir, execBar, db, hashToPath, batchInserter, incremental, minMtime, workers)
	totalTime := time.Since(startTime)

	// Check for cancellation after execution phase
	if ctx.Err() != nil {
		return
	}

	// Only finish/clear the progress bar on successful completion
	execBar.Finish()
	fmt.Println() // Add some space after progress bar

	// Generate perfect accounting summary from results (no manual counters!)
	summary := GenerateAccountingSummary(results, walkErrors)

	// Generate HTML report with perfectly consistent data
	writeHTMLReport(reportPath, summary, totalTime, srcDir, destDir, lastBackupTime, incremental)

	// Print summary with bulletproof accounting
	totalProcessed := len(files)
	fmt.Println()
	color.New(color.FgMagenta, color.Bold).Printf("📊 Final Results\n")
	color.New(color.FgGreen).Printf("   ✅ Copied: %d files\n", summary.Copied)
	color.New(color.FgYellow).Printf("   ⏭️  Skipped: %d files\n", summary.Skipped)
	color.New(color.FgBlue).Printf("   🔄 Duplicates: %d files\n", summary.Duplicates)
	if summary.Errors > 0 {
		color.New(color.FgRed).Printf("   ❌ Errors: %d files\n", summary.Errors)
	} else {
		color.New(color.FgGreen).Printf("   ❌ Errors: %d files\n", summary.Errors)
	}
	color.New(color.FgCyan).Printf("   📁 Total Processed: %d files\n", totalProcessed)

	totalAccounted := summary.Copied + summary.Skipped + summary.Duplicates + summary.Errors
	if totalAccounted == totalProcessed {
		color.New(color.FgGreen, color.Bold).Printf("   ✔ All files accounted for!\n")
	} else {
		color.New(color.FgRed, color.Bold).Printf("   ✖ Mismatch! Accounted: %d, Processed: %d\n", totalAccounted, totalProcessed)
	}

	fmt.Println()
	color.New(color.FgBlue, color.Bold).Printf("📄 Report Generated\n")
	// Print clickable link to HTML report (file://...)
	reportAbs, err := filepath.Abs(reportPath)
	if err == nil {
		link := fmt.Sprintf("file://%s", reportAbs)
		// ANSI hyperlink: \x1b]8;;<url>\x1b\\<text>\x1b]8;;\x1b\\
		ansiLink := fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", link, link)
		color.New(color.FgCyan).Printf("   📄 HTML report: %s\n", ansiLink)
	} else {
		color.New(color.FgCyan).Printf("   📄 HTML report: %s\n", reportPath)
	}

}

// processFilesParallel processes files using a worker pool for concurrent execution
// Maintains result ordering while achieving 4-8x performance improvement on multi-core systems
// Uses in-memory hash set for fast duplicate detection and batch inserter for efficient writes
func processFilesParallel(ctx context.Context, files []FileWithInfo, srcDir, destDir string, bar *progressbar.ProgressBar,
	db *sql.DB, hashToPath map[string]string, batchInserter *BatchInserter, incremental bool, minMtime int64, workers int) []*FileResult {

	// Channels for worker communication
	type job struct {
		index int // Preserve ordering
		file  FileWithInfo
	}

	type resultWithIndex struct {
		index  int
		result *FileResult
	}

	jobs := make(chan job, workers*2)                // Buffered channel for work items
	results := make(chan resultWithIndex, workers*2) // Buffered channel for results

	// Start worker goroutines
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				// Process single file with hash set and batch inserter
				result := processSingleFile(ctx, job.file.Path, job.file.Info, destDir, db, hashToPath, batchInserter, incremental, minMtime)

				// Send result with index to maintain ordering
				select {
				case results <- resultWithIndex{index: job.index, result: result}:
					// Update progress bar with current subdirectory relative to source
					if relPath, err := filepath.Rel(srcDir, job.file.Path); err == nil {
						dir := filepath.Dir(relPath)
						if dir != "." && dir != "/" {
							// Show the subdirectory being processed
							bar.Describe(fmt.Sprintf("%s/", dir))
						} else {
							// File is in root source directory
							bar.Describe("Processing root files")
						}
					} else {
						bar.Describe("Processing files...")
					}
					bar.Add(1)
				case <-ctx.Done():
					return // Context cancelled
				}
			}
		}()
	}

	// Producer: send jobs to workers
	go func() {
		defer close(jobs)
		for i, file := range files {
			select {
			case jobs <- job{index: i, file: file}:
				// Job sent successfully
			case <-ctx.Done():
				return // Context cancelled
			}
		}
	}()

	// Collector: gather results and close channel when done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results in ordered slice with context awareness
	orderedResults := make([]*FileResult, len(files))
	for {
		select {
		case result, ok := <-results:
			if !ok {
				// Channel closed, all results collected
				goto resultsComplete
			}
			orderedResults[result.index] = result.result
		case <-ctx.Done():
			// Context cancelled, stop collecting results
			fmt.Printf("\nExecution phase interrupted\n")
			goto resultsComplete
		}
	}
resultsComplete:

	return orderedResults
}

// processSingleFile handles the processing of a single file (extracted from the original loop)
// Uses in-memory hash set for fast duplicate detection and batch inserter for efficient writes
func processSingleFile(ctx context.Context, file string, info os.FileInfo, destDir string, db *sql.DB, hashToPath map[string]string, batchInserter *BatchInserter,
	incremental bool, minMtime int64) *FileResult {

	// Create FileCandidate (uses cached os.FileInfo, no duplicate syscall)
	candidate := &FileCandidate{
		Path:      file,
		Info:      info,
		Extension: strings.ToLower(filepath.Ext(file)),
		DestDir:   destDir,
	}

	// Classify and process the file using hash set and batch inserter
	result := classifyAndProcessFile(ctx, candidate, db, hashToPath, batchInserter, incremental, minMtime)

	return result
}
