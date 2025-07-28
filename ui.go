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
