// bozobackup: Incremental, deduplicating photo/video backup tool with HTML reporting.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/fatih/color"
	"github.com/manifoldco/promptui"
	"github.com/sqweek/dialog"
)

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

// isGUIAvailable checks if GUI toolkit is available without showing errors
func isGUIAvailable() bool {
	defer func() {
		recover() // Catch any panics from GUI initialization
	}()

	// Check for display environment variables on Linux/Unix
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}

	return true
}

// guiDirectoryPicker opens a native directory selection dialog
func guiDirectoryPicker(title string) (string, error) {
	defer func() {
		recover() // Catch any panics from GUI operations
	}()

	directory, err := dialog.Directory().Title(title).Browse()
	if err != nil {
		return "", err
	}

	// Validate that the selected path is actually a directory
	if info, err := os.Stat(directory); err != nil || !info.IsDir() {
		return "", err
	}

	return directory, nil
}

// interactivePrompt prompts the user for source, destination, and incremental mode
func interactivePrompt(useGUI bool) (string, string, bool) {
	printBanner()

	// Bozo's introduction
	fmt.Println()
	color.New(color.FgCyan, color.Bold).Println("👋 Hey there! I'm Bozo, your backup buddy!")
	fmt.Println()
	color.New(color.FgWhite).Println("   I'm here to help you safely backup your photos and videos.")
	color.New(color.FgWhite).Println("   Here's what I do:")
	color.New(color.FgGreen).Println("   • 📁 Organize files by date into YYYY-MM folders")
	color.New(color.FgBlue).Println("   • 🔍 Skip duplicates using smart hash detection")
	color.New(color.FgYellow).Println("   • ⚡ Process only new/changed files (incremental mode)")
	color.New(color.FgMagenta).Println("   • 📊 Generate a detailed HTML report when done")

	fmt.Println()
	readyPrompt := promptui.Select{
		Label: "Ready to backup your files?",
		Items: []string{"Yes, let's do this!", "No, maybe later"},
	}
	_, ready, err := readyPrompt.Run()
	if err == promptui.ErrInterrupt {
		color.New(color.FgRed, color.Bold).Println("\nInterrupted during prompt. Exiting cleanly.")
		os.Exit(130)
	} else if err != nil {
		log.Fatalf("[FATAL] Ready prompt failed: %v", err)
	}

	if ready == "No, maybe later" {
		color.New(color.FgYellow).Println("\n👋 No worries! Come back when you're ready to backup.")
		os.Exit(0)
	}

	var srcDir, destDir string

	// Try GUI first if enabled and available
	if useGUI && isGUIAvailable() {
		fmt.Println()
		color.New(color.FgCyan, color.Bold).Println("📂 Directory Selection")
		color.New(color.FgBlue).Println("   Opening file picker for SOURCE directory...")
		color.New(color.FgYellow).Println("   (Pick where your photos/videos currently live)")

		srcDir, err = guiDirectoryPicker("Select Source Directory")
		if err == nil {
			fmt.Println()
			color.New(color.FgBlue).Println("   Opening file picker for DESTINATION directory...")
			color.New(color.FgYellow).Println("   (Now pick where I'll organize and backup your files)")

			destDir, err = guiDirectoryPicker("Select Destination Directory")
		}
	}

	// Fallback to text prompts if GUI failed or not available
	if srcDir == "" || destDir == "" {
		if useGUI {
			fmt.Println()
			color.New(color.FgYellow).Println("   GUI picker unavailable, using text prompts instead...")
		}

		fmt.Println()
		color.New(color.FgCyan, color.Bold).Println("📂 Directory Selection")
		color.New(color.FgWhite).Println("   💡 Tip: Copy and paste folder paths from your file manager")
		color.New(color.FgWhite).Println("       Use Ctrl+Shift+V to paste in most terminals")

		prompt := promptui.Prompt{
			Label: "Source directory (where your photos/videos currently live)",
			Validate: func(input string) error {
				info, err := os.Stat(input)
				if err != nil || !info.IsDir() {
					return fmt.Errorf("not a valid directory")
				}
				return nil
			},
		}
		if srcDir == "" {
			srcDir, err = prompt.Run()
			if err == promptui.ErrInterrupt {
				color.New(color.FgRed, color.Bold).Println("\nInterrupted during prompt. Exiting cleanly.")
				os.Exit(130)
			} else if err != nil {
				log.Fatalf("[FATAL] Source directory prompt failed: %v", err)
			}
		}

		if destDir == "" {
			prompt.Label = "Destination directory (where I'll organize and backup your files)"
			destDir, err = prompt.Run()
			if err == promptui.ErrInterrupt {
				color.New(color.FgRed, color.Bold).Println("\nInterrupted during prompt. Exiting cleanly.")
				os.Exit(130)
			} else if err != nil {
				log.Fatalf("[FATAL] Destination directory prompt failed: %v", err)
			}
		}
	}

	// After destination directory, show backup status and database info
	dbPath := filepath.Join(destDir, "bozobackup.db")
	var lastBackupTime time.Time
	var hashCount int

	if info, err := os.Stat(dbPath); err == nil && !info.IsDir() {
		if db, err := sql.Open("sqlite", dbPath); err == nil {
			lastBackupTime, err = getLastBackupTime(db)

			// Get count of existing hashes in database
			var count int
			if err := db.QueryRow("SELECT COUNT(*) FROM file_hashes").Scan(&count); err == nil {
				hashCount = count
			}
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

				fmt.Println()
				color.New(color.FgCyan, color.Bold).Println("📁 Backup Status")
				color.New(color.FgGreen).Printf("   Last backup: %s (%s)\n", agoStr, lastBackupTime.Format("2006-01-02 15:04:05"))
				color.New(color.FgBlue).Printf("   Database contains: %d unique file hashes\n", hashCount)
			} else {
				fmt.Println()
				color.New(color.FgCyan, color.Bold).Println("📁 Backup Status")
				color.New(color.FgYellow).Println("   No previous backup found")
				if hashCount > 0 {
					color.New(color.FgBlue).Printf("   Database contains: %d unique file hashes\n", hashCount)
				}
			}
		}
	} else {
		fmt.Println()
		color.New(color.FgCyan, color.Bold).Println("📁 Backup Status")
		color.New(color.FgYellow).Println("   New backup destination (no existing database)")
	}

	fmt.Println()
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

	// Show the selected mode clearly
	fmt.Println()
	color.New(color.FgMagenta, color.Bold).Println("⚙️  Backup Configuration")
	if incremental {
		color.New(color.FgGreen).Println("   Mode: Incremental backup (only new/changed files)")
		if !lastBackupTime.IsZero() {
			color.New(color.FgBlue).Printf("   Will process files newer than: %s\n", lastBackupTime.Format("2006-01-02 15:04:05"))
		}
	} else {
		color.New(color.FgYellow).Println("   Mode: Full backup (all files will be processed)")
	}

	return srcDir, destDir, incremental
}
