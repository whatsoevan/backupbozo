// bozobackup: Incremental, deduplicating photo/video backup tool with HTML reporting.
package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"os/signal"
	"syscall"

	"html"

	"context"

	"github.com/fatih/color"
	"github.com/manifoldco/promptui"
	"github.com/rwcarlsen/goexif/exif"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

// Estimated size per DB record and minimum DB padding for free space check
const dbRecordEstimate = 512          // bytes per file record
const dbMinPadding = 10 * 1024 * 1024 // 10 MB minimum padding

// allowedExtensions defines which file types are considered for backup
var allowedExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".heic": true,
	".png":  true,
	".mp4":  true,
	".mov":  true,
	".mkv":  true,
	".webm": true,
	".avi":  true,
}

// SkippedFile records a file that was skipped and the reason
// (for HTML reporting and transparency)
type SkippedFile struct {
	Path   string // Absolute file path
	Reason string // Reason for skipping
}

func main() {
	var srcDir, destDir, dbPath, reportPath string
	var incremental bool
	var interactive bool

	var rootCmd = &cobra.Command{
		Use:   "bozobackup",
		Short: "Backup photos and videos with deduplication and reporting",
		Long: `bozobackup is a fast, incremental backup tool for photos and videos.

Features:
- Deduplicates files using SHA256 hashes and an SQLite database (pure Go driver)
- Supports incremental backups (only new/changed files are processed)
- Organizes files into YYYY-MM folders by date
- Supports .jpg, .jpeg, .heic, .mp4, .mov, .mkv, .webm, .avi
- Generates an HTML report of copied, duplicate, and error files
- Skips files already present at the destination
- Handles iPhone .heic photos
- Requires ffprobe for video date extraction
`,
		Example: `  # Basic usage: backup new photos from ~/DCIM to ~/backup_photos
  bozobackup --src ~/DCIM --dest ~/backup_photos

  # Full backup (not incremental)
  bozobackup --src ~/DCIM --dest ~/backup_photos --incremental=false

  # Custom database and report paths
  bozobackup --src ~/DCIM --dest ~/backup_photos --db ~/backup_photos/my.db --report ~/backup_photos/report.html
`,
		Run: func(cmd *cobra.Command, args []string) {
			// If no arguments are supplied, default to interactive mode
			if len(os.Args) == 1 {
				interactive = true
			}
			if !checkExternalTool("ffprobe") {
				fmt.Fprintln(os.Stderr, "[FATAL] Required tool 'ffprobe' not found in PATH. Please install ffmpeg/ffprobe.")
				os.Exit(1)
			}
			if interactive {
				srcDir, destDir, incremental = interactivePrompt()
			}
			// Only check for required directories if not in interactive mode
			if !interactive && (srcDir == "" || destDir == "") {
				log.Fatal("Source and destination directories are required")
			}
			if dbPath == "" {
				dbPath = filepath.Join(destDir, "bozobackup.db")
			}
			if reportPath == "" {
				reportPath = filepath.Join(destDir, fmt.Sprintf("report_%s.html", time.Now().Format("20060102_150405")))
			}

			// Handle interrupts for graceful shutdown using context
			ctx, cancel := context.WithCancel(context.Background())
			interrupt := make(chan os.Signal, 1)
			signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-interrupt
				color.New(color.FgRed, color.Bold).Println("\nInterrupted. Exiting cleanly.")
				cancel()
			}()

			backup(ctx, srcDir, destDir, dbPath, reportPath, incremental)
		},
	}

	rootCmd.Flags().StringVarP(&srcDir, "src", "s", "", "Source directory")
	rootCmd.Flags().StringVarP(&destDir, "dest", "d", "", "Destination directory")
	rootCmd.Flags().StringVar(&dbPath, "db", "", "Path to SQLite database")
	rootCmd.Flags().StringVar(&reportPath, "report", "", "Path to HTML report")
	rootCmd.Flags().BoolVar(&incremental, "incremental", true, "Only process files newer than last backup")
	rootCmd.Flags().BoolVar(&interactive, "interactive", false, "Run in interactive mode (prompts for input)")

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

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

func estimateDBSize(numFiles int) int64 {
	est := int64(numFiles) * dbRecordEstimate
	if est < dbMinPadding {
		return dbMinPadding
	}
	return est
}

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
	bar := progressbar.NewOptions(
		len(files),
		progressbar.OptionSetDescription("Processing"),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetWidth(20),
		progressbar.OptionSetPredictTime(true), // ETA
		progressbar.OptionSetElapsedTime(true), // Elapsed
		progressbar.OptionClearOnFinish(),
	)
	var copied, duplicates, errors int
	var errorList []string
	var copiedFiles [][2]string    // [][src, dst] for HTML report
	var duplicateFiles [][2]string // [][src, dst] for HTML report
	var skippedFiles []SkippedFile // Skipped files and reasons for HTML report
	var totalCopiedSize int64
	var filesToCopy []string // Used for free space estimation

	// First pass: determine which files will be copied and their total size
	for _, file := range files {
		ext := strings.ToLower(filepath.Ext(file))
		if !allowedExtensions[ext] {
			skippedFiles = append(skippedFiles, SkippedFile{Path: file, Reason: "filtered (extension)"})
			continue
		}
		info, err := os.Stat(file)
		if err != nil {
			skippedFiles = append(skippedFiles, SkippedFile{Path: file, Reason: fmt.Sprintf("stat error: %v", err)})
			continue
		}
		if incremental && minMtime > 0 && info.ModTime().Unix() <= minMtime {
			skippedFiles = append(skippedFiles, SkippedFile{Path: file, Reason: "old (not newer than last backup)"})
			continue
		}
		date := getFileDate(file)
		if date.IsZero() {
			skippedFiles = append(skippedFiles, SkippedFile{Path: file, Reason: "no date found"})
			continue
		}
		monthFolder := date.Format("2006-01")
		destMonthDir := filepath.Join(destDir, monthFolder)
		os.MkdirAll(destMonthDir, 0755)
		destFile := filepath.Join(destMonthDir, filepath.Base(file))
		if _, err := os.Stat(destFile); err == nil {
			skippedFiles = append(skippedFiles, SkippedFile{Path: file, Reason: "already present at destination"})
			continue
		}
		filesToCopy = append(filesToCopy, file)
		totalCopiedSize += info.Size()
	}

	// Check free space before copying
	dbEstimate := estimateDBSize(len(filesToCopy))
	requiredSpace := totalCopiedSize + dbEstimate
	free, err := getFreeSpace(destDir)
	if err != nil {
		color.New(color.FgRed).Printf("[FATAL] Could not determine free space for '%s': %v\n", destDir, err)
		os.Exit(1)
	}
	if free < uint64(requiredSpace) {
		color.New(color.FgRed).Printf("[FATAL] Not enough free space in destination. Required: %.2f MB, Available: %.2f MB\n",
			float64(requiredSpace)/(1024*1024), float64(free)/(1024*1024))
		os.Exit(1)
	}

	// Second pass: process files (copy, dedup, record, report)
	for _, file := range files {
		select {
		case <-ctx.Done():
			color.New(color.FgRed, color.Bold).Println("Backup interrupted by user. Writing partial report and exiting.")
			break
		default:
		}
		if ctx.Err() != nil {
			break
		}
		ext := strings.ToLower(filepath.Ext(file))
		if !allowedExtensions[ext] {
			bar.Add(1)
			continue
		}
		info, err := os.Stat(file)
		if err != nil {
			// Only log errors to errorList, not terminal
			errorList = append(errorList, fmt.Sprintf("%s: stat error: %v", file, err))
			bar.Add(1)
			continue
		}
		if incremental && minMtime > 0 && info.ModTime().Unix() <= minMtime {
			bar.Add(1)
			continue
		}
		date := getFileDate(file)
		if date.IsZero() {
			bar.Add(1)
			continue
		}
		monthFolder := date.Format("2006-01")
		destMonthDir := filepath.Join(destDir, monthFolder)
		os.MkdirAll(destMonthDir, 0755)
		destFile := filepath.Join(destMonthDir, filepath.Base(file))
		if _, err := os.Stat(destFile); err == nil {
			bar.Add(1)
			continue
		}
		// Only now compute hash and check for duplicates
		size, mtime := getFileStat(file)
		hash := getFileHash(file)
		if hash == "" {
			// Only log errors to errorList, not terminal
			errorList = append(errorList, fmt.Sprintf("%s: hash error", file))
			errors++
			bar.Add(1)
			continue
		}
		if fileAlreadyProcessed(db, hash) {
			duplicates++
			duplicateFiles = append(duplicateFiles, [2]string{file, destFile})
			bar.Add(1)
			continue
		}
		if err := copyFileAtomic(ctx, file, destFile); err != nil {
			// Only log errors to errorList, not terminal
			errorList = append(errorList, fmt.Sprintf("%s: copy error: %v", file, err))
			errors++
			bar.Add(1)
			if ctx.Err() != nil {
				break
			}
			continue
		}
		insertFileRecord(db, file, destFile, hash, size, mtime)
		copied++
		copiedFiles = append(copiedFiles, [2]string{file, destFile})
		bar.Add(1)
	}

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

func initDB(dbPath string) *sql.DB {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] Could not open database: %v\n", err)
		os.Exit(1)
	}
	sqlStmt := `
	CREATE TABLE IF NOT EXISTS files (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		src_path TEXT,
		dest_path TEXT,
		hash TEXT UNIQUE,
		size INTEGER,
		mtime INTEGER,
		copied_at TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_hash ON files(hash);
	`
	_, err = db.Exec(sqlStmt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] Could not initialize database schema: %v\n", err)
		db.Close()
		os.Exit(1)
	}
	return db
}

func getAllFiles(root string) ([]string, []error) {
	var files []string
	var errors []error
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			errors = append(errors, fmt.Errorf("%s: %v", path, err))
			return nil // continue walking
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files, errors
}

func getFileStat(path string) (int64, int64) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0
	}
	return info.Size(), info.ModTime().Unix()
}

func getFileDate(path string) time.Time {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".jpg" || ext == ".jpeg" {
		if dt, err := getExifDate(path); err == nil {
			return dt
		}
	}
	if ext == ".mp4" || ext == ".mov" || ext == ".mkv" || ext == ".webm" || ext == ".avi" {
		if dt, err := getVideoCreationDate(path); err == nil {
			return dt
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func getExifDate(path string) (time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, err
	}
	defer f.Close()
	x, err := exif.Decode(f)
	if err != nil {
		return time.Time{}, err
	}
	dt, err := x.DateTime()
	return dt, err
}

func getVideoCreationDate(path string) (time.Time, error) {
	cmd := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", path)
	out, err := cmd.Output()
	if err != nil {
		return time.Time{}, err
	}
	type format struct {
		Tags struct {
			CreationTime string `json:"creation_time"`
		} `json:"tags"`
	}
	type ffprobeOut struct {
		Format format `json:"format"`
	}
	var data ffprobeOut
	if err := json.Unmarshal(out, &data); err != nil {
		return time.Time{}, err
	}
	if data.Format.Tags.CreationTime != "" {
		return time.Parse(time.RFC3339, data.Format.Tags.CreationTime)
	}
	return time.Time{}, fmt.Errorf("no creation_time")
}

func getFileHash(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func fileAlreadyProcessed(db *sql.DB, hash string) bool {
	var id int
	err := db.QueryRow("SELECT id FROM files WHERE hash = ?", hash).Scan(&id)
	return err == nil
}

func insertFileRecord(db *sql.DB, src, dest, hash string, size, mtime int64) {
	_, err := db.Exec("INSERT OR IGNORE INTO files (src_path, dest_path, hash, size, mtime, copied_at) VALUES (?, ?, ?, ?, ?, ?)",
		src, dest, hash, size, mtime, time.Now().Format(time.RFC3339))
	if err != nil {
		log.Printf("DB insert error: %v", err)
	}
}

// copyFileAtomic copies a file to a temp file in the destination directory, then renames it atomically.
// If interrupted (ctx.Done), the temp file is deleted and the destination is never partially written.
func copyFileAtomic(ctx context.Context, src, dst string) error {
	tmpDst := dst + ".tmp"
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(tmpDst)
	if err != nil {
		return err
	}
	defer func() {
		out.Close()
		if ctx.Err() != nil {
			os.Remove(tmpDst)
		}
	}()

	buf := make([]byte, 1024*1024)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, readErr := in.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		os.Remove(tmpDst)
		return ctx.Err()
	}
	return os.Rename(tmpDst, dst)
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

// checkExternalTool checks if a tool is available in PATH
func checkExternalTool(tool string) bool {
	_, err := exec.LookPath(tool)
	return err == nil
}

// getLastBackupTime returns the most recent copied_at time from the DB, or zero if none
func getLastBackupTime(db *sql.DB) (time.Time, error) {
	row := db.QueryRow("SELECT MAX(copied_at) FROM files WHERE copied_at IS NOT NULL")
	var last string
	err := row.Scan(&last)
	if err != nil || last == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, last)
	if err != nil {
		return time.Time{}, nil
	}
	return parsed, nil
}

// printBanner prints a colored ASCII art banner for bozobackup
func printBanner() {
	banner := `

	██████╗  ██████╗ ███████╗ ██████╗ ██████╗  █████╗  ██████╗██╗  ██╗██╗   ██╗██████╗ 
	██╔══██╗██╔═══██╗╚══███╔╝██╔═══██╗██╔══██╗██╔══██╗██╔════╝██║ ██╔╝██║   ██║██╔══██╗
	██████╔╝██║   ██║  ███╔╝ ██║   ██║██████╔╝███████║██║     █████╔╝ ██║   ██║██████╔╝
	██╔══██╗██║   ██║ ███╔╝  ██║   ██║██╔══██╗██╔══██║██║     ██╔═██╗ ██║   ██║██╔═══╝ 
	██████╔╝╚██████╔╝███████╗╚██████╔╝██████╔╝██║  ██║╚██████╗██║  ██╗╚██████╔╝██║     
	╚═════╝  ╚═════╝ ╚══════╝ ╚═════╝ ╚═════╝ ╚═╝  ╚═╝ ╚═════╝╚═╝  ╚═╝ ╚═════╝ ╚═╝     
																					   
	  
`
	color.New(color.FgBlack, color.Bold).Println(banner)
}

// interactivePrompt prompts the user for source, destination, and incremental mode
func interactivePrompt() (string, string, bool) {
	printBanner()

	prompt := promptui.Prompt{
		Label: "Source directory",
		Validate: func(input string) error {
			info, err := os.Stat(input)
			if err != nil || !info.IsDir() {
				return fmt.Errorf("not a valid directory")
			}
			return nil
		},
	}
	srcDir, err := prompt.Run()
	if err == promptui.ErrInterrupt {
		color.New(color.FgRed, color.Bold).Println("\nInterrupted during prompt. Exiting cleanly.")
		os.Exit(130)
	} else if err != nil {
		log.Fatalf("[FATAL] Source directory prompt failed: %v", err)
	}

	prompt.Label = "Destination directory"
	destDir, err := prompt.Run()
	if err == promptui.ErrInterrupt {
		color.New(color.FgRed, color.Bold).Println("\nInterrupted during prompt. Exiting cleanly.")
		os.Exit(130)
	} else if err != nil {
		log.Fatalf("[FATAL] Destination directory prompt failed: %v", err)
	}

	// After destination directory, print last backup time if available
	dbPath := filepath.Join(destDir, "bozobackup.db")
	if info, err := os.Stat(dbPath); err == nil && !info.IsDir() {
		if db, err := sql.Open("sqlite", dbPath); err == nil {
			lastBackupTime, err := getLastBackupTime(db)
			db.Close()
			if err == nil && !lastBackupTime.IsZero() {
				delta := time.Since(lastBackupTime)
				days := int(delta.Hours()) / 24
				hours := int(delta.Hours()) % 24
				minutes := int(delta.Minutes()) % 60
				var agoStr string
				if days > 0 {
					agoStr = fmt.Sprintf("%d days, %d hours, %d minutes ago", days, hours, minutes)
				} else if hours > 0 {
					agoStr = fmt.Sprintf("%d hours, %d minutes ago", hours, minutes)
				} else if minutes > 0 {
					agoStr = fmt.Sprintf("%d minutes ago", minutes)
				} else {
					agoStr = "just now"
				}
				color.New(color.FgGreen).Printf("Last backup was %s (%s)\n", agoStr, lastBackupTime.Format(time.RFC3339))
			}
		}
	}

	incPrompt := promptui.Select{
		Label: "Incremental backup (only new/changed files)?",
		Items: []string{"Yes", "No"},
	}
	_, inc, err := incPrompt.Run()
	if err == promptui.ErrInterrupt {
		color.New(color.FgRed, color.Bold).Println("\nInterrupted during prompt. Exiting cleanly.")
		os.Exit(130)
	} else if err != nil {
		log.Fatalf("[FATAL] Incremental prompt failed: %v", err)
	}
	incremental := inc == "Yes"

	return srcDir, destDir, incremental
}
