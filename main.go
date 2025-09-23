// bozobackup: Incremental, deduplicating photo/video backup tool with HTML reporting.
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"os/signal"
	"syscall"

	"context"

	"github.com/fatih/color"
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

func estimateDBSize(numFiles int) int64 {
	est := int64(numFiles) * dbRecordEstimate
	if est < dbMinPadding {
		return dbMinPadding
	}
	return est
}

// checkExternalTool checks if a tool is available in PATH
func checkExternalTool(tool string) bool {
	_, err := exec.LookPath(tool)
	return err == nil
}

func main() {
	var srcDir, destDir, dbPath, reportPath string
	var incremental bool
	var interactive bool
	var workers int

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

			backup(ctx, srcDir, destDir, dbPath, reportPath, incremental, workers)
		},
	}

	rootCmd.Flags().StringVarP(&srcDir, "src", "s", "", "Source directory")
	rootCmd.Flags().StringVarP(&destDir, "dest", "d", "", "Destination directory")
	rootCmd.Flags().StringVar(&dbPath, "db", "", "Path to SQLite database")
	rootCmd.Flags().StringVar(&reportPath, "report", "", "Path to HTML report")
	rootCmd.Flags().BoolVar(&incremental, "incremental", true, "Only process files newer than last backup")
	rootCmd.Flags().BoolVar(&interactive, "interactive", false, "Run in interactive mode (prompts for input)")
	rootCmd.Flags().IntVar(&workers, "workers", runtime.NumCPU(), "Number of parallel workers (default: CPU cores)")

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
