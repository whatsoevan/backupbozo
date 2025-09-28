// backupbozo: Incremental, deduplicating photo/video backup tool with HTML reporting.
package main

import (
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	fileSizeUnits = "KMGTPE"
)

// QuoteContext consolidates all data needed for personalized quote generation
type QuoteContext struct {
	Summary        AccountingSummary
	LastBackupTime time.Time
	IsFirstBackup  bool
	ProcessingTime time.Duration
	OldestFileAge  time.Duration
	IsInterrupted  bool
}

const reportCSS = `    <style>
        :root {
            --background: 0 0% 100%;
            --foreground: 222.2 84% 4.9%;
            --card: 0 0% 100%;
            --card-foreground: 222.2 84% 4.9%;
            --popover: 0 0% 100%;
            --popover-foreground: 222.2 84% 4.9%;
            --primary: 222.2 47.4% 11.2%;
            --primary-foreground: 210 40% 98%;
            --secondary: 210 40% 96%;
            --secondary-foreground: 222.2 84% 4.9%;
            --muted: 210 40% 96%;
            --muted-foreground: 215.4 16.3% 46.9%;
            --accent: 210 40% 96%;
            --accent-foreground: 222.2 84% 4.9%;
            --destructive: 0 84.2% 60.2%;
            --destructive-foreground: 210 40% 98%;
            --border: 214.3 31.8% 91.4%;
            --input: 214.3 31.8% 91.4%;
            --ring: 222.2 84% 4.9%;
            --radius: 0.5rem;
        }

        * {
            box-sizing: border-box;
        }

        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
            line-height: 1.5;
            color: hsl(var(--foreground));
            background-color: hsl(var(--background));
            margin: 0;
            padding: 20px;
        }

        .container {
            max-width: 1200px;
            margin: 0 auto;
        }

        h1 {
            font-size: 2.25rem;
            font-weight: 700;
            margin-bottom: 2rem;
            color: hsl(var(--foreground));
        }


        .controls {
            display: flex;
            gap: 1rem;
            margin-bottom: 1rem;
            flex-wrap: wrap;
            align-items: center;
        }

        .search-input {
            flex: 1;
            min-width: 200px;
            padding: 0.5rem 0.75rem;
            border: 1px solid hsl(var(--border));
            border-radius: var(--radius);
            background: hsl(var(--background));
            color: hsl(var(--foreground));
            font-size: 0.875rem;
        }

        .search-input:focus {
            outline: none;
            ring: 2px;
            ring-color: hsl(var(--ring));
            border-color: hsl(var(--ring));
        }

        .filter-buttons {
            display: flex;
            gap: 0.5rem;
            flex-wrap: wrap;
        }

        .filter-btn {
            padding: 0.375rem 0.75rem;
            border: 1px solid hsl(var(--border));
            border-radius: var(--radius);
            background: hsl(var(--secondary));
            color: hsl(var(--secondary-foreground));
            font-size: 0.875rem;
            cursor: pointer;
            transition: all 0.2s;
        }

        .filter-btn:hover {
            background: hsl(var(--accent));
        }

        .filter-btn.active {
            background: hsl(var(--primary));
            color: hsl(var(--primary-foreground));
        }

        .table-container {
            border: 1px solid hsl(var(--border));
            border-radius: var(--radius);
            overflow: hidden;
            background: hsl(var(--card));
        }

        table {
            width: 100%;
            border-collapse: collapse;
        }

        .table-header {
            background: hsl(var(--muted));
            position: sticky;
            top: 0;
            z-index: 10;
        }

        .table-body {
            max-height: 600px;
            overflow-y: auto;
        }

        th, td {
            text-align: left;
            padding: 0.75rem;
            border-bottom: 1px solid hsl(var(--border));
        }

        th {
            font-weight: 600;
            color: hsl(var(--foreground));
            cursor: pointer;
            user-select: none;
            white-space: nowrap;
        }

        th:hover {
            background: hsl(var(--accent));
        }

        .sort-indicator {
            margin-left: 0.5rem;
            opacity: 0.5;
        }

        .sort-indicator.active {
            opacity: 1;
        }

        td {
            color: hsl(var(--foreground));
        }

        .file-path {
            max-width: 250px;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
            word-break: break-all;
        }

        .file-path a {
            color: hsl(var(--primary));
            text-decoration: none;
        }

        .file-path a:hover {
            text-decoration: underline;
        }

        .status-badge {
            display: inline-flex;
            align-items: center;
            padding: 0.25rem 0.5rem;
            border-radius: calc(var(--radius) - 2px);
            font-size: 0.75rem;
            font-weight: 500;
            white-space: nowrap;
        }

        .status-copied {
            background: hsl(142 76% 36% / 0.1);
            color: hsl(142 76% 36%);
        }

        .status-skipped {
            background: hsl(45 93% 47% / 0.1);
            color: hsl(45 93% 47%);
        }

        .status-duplicate {
            background: hsl(221 83% 53% / 0.1);
            color: hsl(221 83% 53%);
        }

        .status-error {
            background: hsl(var(--destructive) / 0.1);
            color: hsl(var(--destructive));
        }

        .file-size {
            font-variant-numeric: tabular-nums;
            text-align: right;
        }

        tr:hover {
            background: hsl(var(--muted) / 0.5);
        }

        .hidden {
            display: none !important;
        }

        /* Mascot header styles */
        .mascot-header {
            text-align: center;
            margin-bottom: 2rem;
            padding: 1rem;
        }

        .mascot-icon {
            width: 80px;
            height: 80px;
            margin: 1rem auto;
            display: block;
        }

        .mascot-quote {
            font-size: 1rem;
            color: hsl(var(--muted-foreground));
            margin: 1rem 0;
            font-style: italic;
        }

        /* Summary badges styles */
        .summary-badges {
            display: flex;
            flex-direction: column;
            gap: 0.75rem;
            margin: 1.5rem 0;
        }

        .badge-row {
            display: flex;
            justify-content: center;
            gap: 0.75rem;
            flex-wrap: wrap;
        }

        .summary-badge {
            display: inline-flex;
            flex-direction: column;
            align-items: center;
            padding: 0.75rem;
            border-radius: var(--radius);
            min-width: 80px;
            text-align: center;
            font-weight: 500;
            border: 1px solid;
        }

        .badge-label {
            font-size: 0.75rem;
            opacity: 0.8;
            margin-bottom: 0.25rem;
        }

        .badge-value {
            font-size: 1.1rem;
            font-weight: 700;
        }

        /* Badge color themes */
        .badge-total {
            background: hsl(210 40% 96%);
            color: hsl(222.2 84% 4.9%);
            border-color: hsl(214.3 31.8% 91.4%);
        }

        .badge-data {
            background: hsl(221 83% 53% / 0.1);
            color: hsl(221 83% 53%);
            border-color: hsl(221 83% 53% / 0.3);
        }

        .badge-time {
            background: hsl(262 83% 58% / 0.1);
            color: hsl(262 83% 58%);
            border-color: hsl(262 83% 58% / 0.3);
        }

        .badge-copied {
            background: hsl(142 76% 36% / 0.1);
            color: hsl(142 76% 36%);
            border-color: hsl(142 76% 36% / 0.3);
        }

        .badge-duplicate {
            background: hsl(221 83% 53% / 0.1);
            color: hsl(221 83% 53%);
            border-color: hsl(221 83% 53% / 0.3);
        }

        .badge-skipped {
            background: hsl(45 93% 47% / 0.1);
            color: hsl(45 93% 47%);
            border-color: hsl(45 93% 47% / 0.3);
        }

        .badge-error {
            background: hsl(var(--destructive) / 0.1);
            color: hsl(var(--destructive));
            border-color: hsl(var(--destructive) / 0.3);
        }

        @media (max-width: 768px) {
            .controls {
                flex-direction: column;
                align-items: stretch;
            }

            .search-input {
                min-width: unset;
            }

            .file-path {
                max-width: 150px;
            }

            th, td {
                padding: 0.5rem;
                font-size: 0.875rem;
            }

            .mascot-icon {
                width: 60px;
                height: 60px;
            }

            .mascot-quote {
                font-size: 0.9rem;
                padding: 0 1rem;
            }

            .badge-row {
                gap: 0.5rem;
            }

            .summary-badge {
                min-width: 70px;
                padding: 0.5rem;
            }

            .badge-label {
                font-size: 0.7rem;
            }

            .badge-value {
                font-size: 1rem;
            }
        }
    </style>`

const reportJavaScript = `        <script>
            document.addEventListener('DOMContentLoaded', function() {
                const searchInput = document.getElementById('searchInput');
                const filterButtons = document.querySelectorAll('.filter-btn');
                const tableBody = document.getElementById('fileTableBody');
                const sortHeaders = document.querySelectorAll('th[data-sort]');

                let currentFilter = 'all';
                let currentSort = { column: null, direction: 'asc' };

                // Search functionality
                searchInput.addEventListener('input', function() {
                    filterAndSearch();
                });

                // Filter functionality
                filterButtons.forEach(btn => {
                    btn.addEventListener('click', function() {
                        filterButtons.forEach(b => b.classList.remove('active'));
                        this.classList.add('active');
                        currentFilter = this.dataset.filter;
                        filterAndSearch();
                    });
                });

                // Sort functionality
                sortHeaders.forEach(header => {
                    header.addEventListener('click', function() {
                        const column = this.dataset.sort;

                        if (currentSort.column === column) {
                            currentSort.direction = currentSort.direction === 'asc' ? 'desc' : 'asc';
                        } else {
                            currentSort.column = column;
                            currentSort.direction = 'asc';
                        }

                        updateSortIndicators();
                        sortTable();
                    });
                });

                function filterAndSearch() {
                    const searchTerm = searchInput.value.toLowerCase();
                    const rows = tableBody.querySelectorAll('tr');

                    rows.forEach(row => {
                        const status = row.dataset.status;
                        const path = row.dataset.path.toLowerCase();

                        const matchesFilter = currentFilter === 'all' || status === currentFilter;
                        const matchesSearch = searchTerm === '' || path.includes(searchTerm);

                        row.style.display = matchesFilter && matchesSearch ? '' : 'none';
                    });
                }

                function updateSortIndicators() {
                    sortHeaders.forEach(header => {
                        const indicator = header.querySelector('.sort-indicator');
                        if (header.dataset.sort === currentSort.column) {
                            indicator.textContent = currentSort.direction === 'asc' ? '↑' : '↓';
                            indicator.classList.add('active');
                        } else {
                            indicator.textContent = '↕';
                            indicator.classList.remove('active');
                        }
                    });
                }

                function sortTable() {
                    const rows = Array.from(tableBody.querySelectorAll('tr'));

                    rows.sort((a, b) => {
                        let aVal, bVal;

                        switch(currentSort.column) {
                            case 'path':
                                aVal = a.dataset.path;
                                bVal = b.dataset.path;
                                break;
                            case 'status':
                                aVal = a.dataset.status;
                                bVal = b.dataset.status;
                                break;
                            case 'destination':
                                aVal = a.cells[2].textContent;
                                bVal = b.cells[2].textContent;
                                break;
                            case 'size':
                                aVal = parseSizeForSort(a.cells[3].textContent);
                                bVal = parseSizeForSort(b.cells[3].textContent);
                                break;
                            case 'details':
                                aVal = a.cells[4].textContent;
                                bVal = b.cells[4].textContent;
                                break;
                            default:
                                return 0;
                        }

                        if (currentSort.column === 'size') {
                            return currentSort.direction === 'asc' ? aVal - bVal : bVal - aVal;
                        }

                        const comparison = aVal.localeCompare(bVal);
                        return currentSort.direction === 'asc' ? comparison : -comparison;
                    });

                    rows.forEach(row => tableBody.appendChild(row));
                }

                function parseSizeForSort(sizeText) {
                    if (sizeText === '-') return 0;

                    const matches = sizeText.match(/^([\d.]+)\s*([KMGTPE]?)B$/);
                    if (!matches) return 0;

                    const value = parseFloat(matches[1]);
                    const unit = matches[2];

                    const multipliers = { '': 1, 'K': 1024, 'M': 1024*1024, 'G': 1024*1024*1024, 'T': 1024*1024*1024*1024 };
                    return value * (multipliers[unit] || 1);
                }
            });
        </script>`

// embedIconAsBase64 reads the icon.webp file and returns it as a base64 data URL
func embedIconAsBase64() string {
	iconPath := "icon.webp"
	file, err := os.Open(iconPath)
	if err != nil {
		log.Printf("Could not read icon file: %v", err)
		return ""
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		log.Printf("Could not read icon data: %v", err)
		return ""
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return "data:image/webp;base64," + encoded
}

// generateTimeContext creates the first sentence about timing
func generateTimeContext(ctx QuoteContext) string {
	if ctx.IsFirstBackup {
		// First backup - talk about memories saved
		templates := []string{
			"You saved %s worth of memories, they're backed up and organized now!",
			"Your entire %s collection is now safe and sound!",
			"Got %s of precious files secured and protected!",
			"That's %s worth of memories safely stored away!",
		}
		ageStr := formatTimeDuration(ctx.OldestFileAge)
		return fmt.Sprintf(templates[rand.Intn(len(templates))], ageStr)
	} else {
		// Subsequent backup - talk about time since last backup
		timeSince := time.Since(ctx.LastBackupTime)
		timeStr := formatTimeDuration(timeSince)

		if timeSince < 30*24*time.Hour {
			// Recent backup (< 1 month)
			templates := []string{
				"Last backup was %s ago, way to keep on top of things!",
				"Been %s since we last met, staying organized!",
				"Back after %s - love the consistency!",
			}
			return fmt.Sprintf(templates[rand.Intn(len(templates))], timeStr)
		} else {
			// Longer gap (>= 1 month)
			templates := []string{
				"It's been %s since your last backup, nice to see you back!",
				"Been %s since we last met - missed you!",
				"Welcome back after %s away!",
				"Good to see you again after %s!",
			}
			return fmt.Sprintf(templates[rand.Intn(len(templates))], timeStr)
		}
	}
}

// generateResultContext creates the second sentence about backup results
func generateResultContext(ctx QuoteContext) string {

	// Calculate percentages for context
	duplicatePercent := 0.0
	errorPercent := 0.0
	skippedPercent := 0.0

	if ctx.Summary.TotalFiles > 0 {
		skippedPercent = float64(ctx.Summary.Skipped) / float64(ctx.Summary.TotalFiles)
		duplicatePercent = float64(ctx.Summary.Duplicates) / float64(ctx.Summary.TotalFiles)
		errorPercent = float64(ctx.Summary.Errors) / float64(ctx.Summary.TotalFiles)
	}

	if errorPercent > 0.1 {
		// >10% errors - encouraging tone
		templates := []string{
			"Hit %d bumps but still saved %d files - resilience!",
			"Powered through %d issues to secure %d files!",
			"Battled %d tricky files but backed up %d successfully!",
		}
		return fmt.Sprintf(templates[rand.Intn(len(templates))], ctx.Summary.Errors, ctx.Summary.TotalFiles)
	} else if ctx.Summary.Copied == 0 {
		// Large backup - achievement focus
		templates := []string{
			"But...huh? I didn't find anything good to copy.",
		}
		return fmt.Sprintf(templates[rand.Intn(len(templates))], ctx.Summary.Copied)
	} else if duplicatePercent > 0.1 {
		// >30% duplicates - organization focus
		templates := []string{
			"Found %d duplicates among %d files - it's a good thing I caught those! Otherwise you'd double up.",
		}
		return fmt.Sprintf(templates[rand.Intn(len(templates))], ctx.Summary.Duplicates, ctx.Summary.TotalFiles)
	} else if skippedPercent > 0.9 {
		// >30% duplicates - organization focus
		templates := []string{
			"We skipped %d files, so that made things a breeze!",
		}
		return fmt.Sprintf(templates[rand.Intn(len(templates))], ctx.Summary.Skipped)
	} else {
		// Standard/clean backup
		templates := []string{
			"%d files processed without breaking a sweat!",
			"Smooth sailing with %d files backed up!",
			"Perfect run with %d files secured!",
			"%d files, zero drama - perfectly organized!",
		}
		return fmt.Sprintf(templates[rand.Intn(len(templates))], ctx.Summary.Copied)
	}
}

// generatePersonalizedQuote creates personalized two-sentence quotes
func generatePersonalizedQuote(ctx QuoteContext) string {
	// Handle interrupted backups with special quotes
	if ctx.IsInterrupted {
		return generateInterruptedQuote(ctx)
	}

	sentence1 := generateTimeContext(ctx)
	sentence2 := generateResultContext(ctx)
	return sentence1 + " " + sentence2
}

// generateInterruptedQuote creates special quotes for interrupted backups
func generateInterruptedQuote(ctx QuoteContext) string {
	templates := []string{
		"Got %d files sorted before the interruption. Let's restart and finish the job!",
		"%d files were sorted before the interruption. Let's pick up where we left off!",
	}
	return fmt.Sprintf(templates[rand.Intn(len(templates))], ctx.Summary.Copied)
}

// createQuoteContext builds a QuoteContext from backup results
func createQuoteContext(summary AccountingSummary, lastBackupTime time.Time, totalTime time.Duration, incremental bool, isInterrupted bool) QuoteContext {
	// Calculate meaningful values for quote generation
	totalFiles := len(summary.CopiedFiles) + len(summary.DuplicateFiles) + len(summary.SkippedFiles) + len(summary.ErrorList)

	// Determine if this looks like a first backup (high percentage of copied files or no last backup time)
	copiedCount := len(summary.CopiedFiles)
	isFirstBackup := (!incremental || lastBackupTime.IsZero()) || (totalFiles > 0 && (float64(copiedCount)/float64(totalFiles) > 0.8))

	// Calculate oldest file age by examining copied files
	var oldestFileAge time.Duration = 0
	now := time.Now()
	for _, pair := range summary.CopiedFiles {
		if info, err := os.Stat(pair[0]); err == nil {
			age := now.Sub(info.ModTime())
			if age > oldestFileAge {
				oldestFileAge = age
			}
		}
	}
	if oldestFileAge == 0 {
		oldestFileAge = 24 * time.Hour // fallback
	}

	return QuoteContext{
		Summary:        summary,
		LastBackupTime: lastBackupTime,
		IsFirstBackup:  isFirstBackup,
		ProcessingTime: totalTime,
		OldestFileAge:  oldestFileAge,
		IsInterrupted:  isInterrupted,
	}
}

// formatTimeDuration formats duration into human-readable time spans
func formatTimeDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	if days > 365 {
		years := days / 365
		if years == 1 {
			return "1 year"
		}
		return fmt.Sprintf("%d years", years)
	} else if days > 30 {
		months := days / 30
		if months == 1 {
			return "1 month"
		}
		return fmt.Sprintf("%d months", months)
	} else if days > 0 {
		if days == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", days)
	} else if d.Hours() > 1 {
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}
	return "moments"
}

// writeBadge writes a single summary badge with the given type, label, and value
func writeBadge(f *os.File, badgeType, label, value string) {
	fmt.Fprintf(f, `
                <span class="summary-badge badge-%s">
                    <span class="badge-label">%s</span>
                    <span class="badge-value">%s</span>
                </span>`, badgeType, label, value)
}

// writeSummaryBadges generates colored statistics badges
func writeSummaryBadges(f *os.File, summary AccountingSummary, totalTime time.Duration) {
	totalFiles := len(summary.CopiedFiles) + len(summary.DuplicateFiles) + len(summary.SkippedFiles) + len(summary.ErrorList)

	// Calculate total data size from copied files
	var totalBytes int64
	for _, pair := range summary.CopiedFiles {
		if info, err := os.Stat(pair[0]); err == nil {
			totalBytes += info.Size()
		}
	}

	f.WriteString(`
        <div class="summary-badges">
            <div class="badge-row">`)

	// Always show all 7 badges in single row
	writeBadge(f, "total", "Total Files", fmt.Sprintf("%d", totalFiles))
	writeBadge(f, "data", "Data Size", formatFileSize(totalBytes))
	writeBadge(f, "time", "Time Taken", formatDuration(totalTime))
	writeBadge(f, "copied", "Copied", fmt.Sprintf("%d", len(summary.CopiedFiles)))
	writeBadge(f, "duplicate", "Duplicates", fmt.Sprintf("%d", len(summary.DuplicateFiles)))
	writeBadge(f, "skipped", "Skipped", fmt.Sprintf("%d", len(summary.SkippedFiles)))
	writeBadge(f, "error", "Errors", fmt.Sprintf("%d", len(summary.ErrorList)))

	f.WriteString(`
            </div>
        </div>`)
}

// formatDuration formats time.Duration into human-readable format
func formatDuration(d time.Duration) string {
	if d.Hours() >= 1 {
		return fmt.Sprintf("%.1fh", d.Hours())
	} else if d.Minutes() >= 1 {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// writeHTMLReport generates a detailed HTML report of the backup session
// Features a modern table-based layout with search, filtering, and sorting
func writeHTMLReport(path string, summary AccountingSummary, totalTime time.Duration, srcRoot, destRoot string, lastBackupTime time.Time, incremental bool, isInterrupted bool) {
	f, err := os.Create(path)
	if err != nil {
		log.Printf("Could not create report: %v", err)
		return
	}
	defer f.Close()

	// Create quote context for personalized quotes
	ctx := createQuoteContext(summary, lastBackupTime, totalTime, incremental, isInterrupted)

	// Write HTML header with embedded CSS and JavaScript
	writeHTMLHeader(f, ctx)

	// Write table with all file data
	writeFileTable(f, summary, srcRoot, destRoot)

	// Close HTML
	f.WriteString("</body></html>")
}

// writeHTMLHeader writes the HTML header with embedded CSS and JavaScript
func writeHTMLHeader(f *os.File, ctx QuoteContext) {
	f.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>backupbozo report</title>
`)
	f.WriteString(reportCSS)
	f.WriteString(`
</head>
<body>
    <div class="container">
        <div class="mascot-header">
            <h1>Backup Report</h1>`)

	// Add mascot icon
	iconData := embedIconAsBase64()
	if iconData != "" {
		fmt.Fprintf(f, `
            <img src="%s" alt="Backup Mascot" class="mascot-icon">`, iconData)
	}

	// Generate personalized quote using context
	quote := generatePersonalizedQuote(ctx)
	fmt.Fprintf(f, `
            <p class="mascot-quote">%s</p>`, html.EscapeString(quote))

	// Add summary badges
	f.WriteString(``)
	writeSummaryBadges(f, ctx.Summary, ctx.ProcessingTime)

	f.WriteString(`
        </div>`)
}

// writeFileTable writes the main file table with all processed files
func writeFileTable(f *os.File, summary AccountingSummary, srcRoot, destRoot string) {
	f.WriteString(`
        <div class="controls">
            <input type="text" class="search-input" placeholder="Search files..." id="searchInput">
            <div class="filter-buttons">
                <button class="filter-btn active" data-filter="all">All</button>
                <button class="filter-btn" data-filter="copied">Copied</button>
                <button class="filter-btn" data-filter="duplicate">Duplicates</button>
                <button class="filter-btn" data-filter="skipped">Skipped</button>
                <button class="filter-btn" data-filter="error">Errors</button>
            </div>
        </div>

        <div class="table-container">
            <table>
                <thead class="table-header">
                    <tr>
                        <th data-sort="path">File Path<span class="sort-indicator">↕</span></th>
                        <th data-sort="status">Status<span class="sort-indicator">↕</span></th>
                        <th data-sort="destination">Destination<span class="sort-indicator">↕</span></th>
                        <th data-sort="size">Size<span class="sort-indicator">↕</span></th>
                        <th data-sort="details">Details<span class="sort-indicator">↕</span></th>
                    </tr>
                </thead>
                <tbody class="table-body" id="fileTableBody">`)

	// Add copied files
	for _, pair := range summary.CopiedFiles {
		srcRel := makeRelativePath(pair[0], srcRoot)
		destRel := makeRelativePath(pair[1], destRoot)
		writeTableRow(f, srcRel, pair[0], "copied", destRel, pair[1], getFileSize(pair[0]), "Successfully copied")
	}

	// Add duplicate files
	for _, pair := range summary.DuplicateFiles {
		srcRel := makeRelativePath(pair[0], srcRoot)
		existingRel := makeRelativePath(pair[1], destRoot)
		writeTableRow(f, srcRel, pair[0], "duplicate", existingRel, pair[1], getFileSize(pair[0]), "Duplicate of existing file")
	}

	// Add skipped files
	for _, skipped := range summary.SkippedFiles {
		srcRel := makeRelativePath(skipped.Path, srcRoot)
		writeTableRow(f, srcRel, skipped.Path, "skipped", "", "", getFileSize(skipped.Path), skipped.Reason)
	}

	// Add error files
	for _, errorMsg := range summary.ErrorList {
		parts := strings.SplitN(errorMsg, ": ", 2)
		path := parts[0]
		details := errorMsg
		if len(parts) > 1 {
			details = parts[1]
		}
		srcRel := makeRelativePath(path, srcRoot)
		writeTableRow(f, srcRel, path, "error", "", "", getFileSize(path), details)
	}

	f.WriteString(`                </tbody>
            </table>
        </div>`)

	// Add JavaScript for search, filter, and sort functionality
	writeJavaScript(f)
}

// makeRelativePath creates a relative path from the full path, including the root folder name
// Example: makeRelativePath("/home/user/photos/IMG_001.jpg", "/home/user/photos") -> "photos/IMG_001.jpg"
func makeRelativePath(fullPath, rootPath string) string {
	if fullPath == "" || rootPath == "" {
		return fullPath
	}

	// Clean both paths to handle any inconsistencies
	cleanFull := filepath.Clean(fullPath)
	cleanRoot := filepath.Clean(rootPath)

	// Get the relative path
	relPath, err := filepath.Rel(cleanRoot, cleanFull)
	if err != nil {
		// If we can't make it relative, return the original path
		return fullPath
	}

	// Include the root folder name in the display
	rootName := filepath.Base(cleanRoot)
	if relPath == "." {
		// The file is in the root directory itself
		return rootName
	}

	return filepath.Join(rootName, relPath)
}

// writeTableRow writes a single table row with clickable file links
func writeTableRow(f *os.File, pathDisplay, pathAbsolute, status, destDisplay, destAbsolute, size, details string) {
	escapedPathDisplay := html.EscapeString(pathDisplay)
	escapedPathAbsolute := html.EscapeString(pathAbsolute)
	escapedDestDisplay := html.EscapeString(destDisplay)
	escapedDestAbsolute := html.EscapeString(destAbsolute)
	escapedDetails := html.EscapeString(details)

	// Create source cell with clickable link if absolute path exists
	var sourceCell string
	if pathAbsolute != "" {
		sourceCell = fmt.Sprintf(`<a href="file://%s" title="Open %s">%s</a>`,
			escapedPathAbsolute, escapedPathAbsolute, escapedPathDisplay)
	} else {
		sourceCell = escapedPathDisplay
	}

	// Create destination cell with clickable link if absolute path exists
	var destCell string
	if destAbsolute != "" {
		destCell = fmt.Sprintf(`<a href="file://%s" title="Open %s">%s</a>`,
			escapedDestAbsolute, escapedDestAbsolute, escapedDestDisplay)
	} else {
		destCell = escapedDestDisplay
	}

	fmt.Fprintf(f, `
                    <tr data-status="%s" data-path="%s">
                        <td class="file-path">%s</td>
                        <td><span class="status-badge status-%s">%s</span></td>
                        <td class="file-path">%s</td>
                        <td class="file-size">%s</td>
                        <td>%s</td>
                    </tr>`,
		status, strings.ToLower(escapedPathDisplay),
		sourceCell,
		status, strings.Title(status),
		destCell,
		size,
		escapedDetails)
}

// getFileSize attempts to get file size, returns "-" if unavailable
func getFileSize(path string) string {
	if path == "" {
		return "-"
	}

	info, err := os.Stat(path)
	if err != nil {
		return "-"
	}

	return formatFileSize(info.Size())
}

// formatFileSize formats bytes into human-readable format
func formatFileSize(bytes int64) string {
	if bytes == 0 {
		return "-"
	}

	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), fileSizeUnits[exp])
}

// writeJavaScript writes the JavaScript for search, filter, and sort functionality
func writeJavaScript(f *os.File) {
	f.WriteString(reportJavaScript)
	f.WriteString(`
    </div>`)
}
