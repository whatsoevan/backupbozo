// bozobackup: Incremental, deduplicating photo/video backup tool with HTML reporting.
package main

import (
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SkippedFile type is defined in pipeline.go with other processing structures

// writeHTMLReport generates a detailed HTML report of the backup session
// Features a modern table-based layout with search, filtering, and sorting
func writeHTMLReport(path string, summary AccountingSummary, totalTime time.Duration, srcRoot, destRoot string) {
	f, err := os.Create(path)
	if err != nil {
		log.Printf("Could not create report: %v", err)
		return
	}
	defer f.Close()

	// Write HTML header with embedded CSS and JavaScript
	writeHTMLHeader(f)

	// Write summary cards
	writeSummaryCards(f, summary, totalTime)

	// Write table with all file data
	writeFileTable(f, summary, srcRoot, destRoot)

	// Close HTML
	f.WriteString("</body></html>")
}

// writeHTMLHeader writes the HTML header with embedded CSS and JavaScript
func writeHTMLHeader(f *os.File) {
	f.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>bozobackup Report</title>
    <style>
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

        .summary-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 1rem;
            margin-bottom: 2rem;
        }

        .summary-card {
            background: hsl(var(--card));
            border: 1px solid hsl(var(--border));
            border-radius: var(--radius);
            padding: 1.5rem;
            box-shadow: 0 1px 3px 0 rgba(0, 0, 0, 0.1), 0 1px 2px 0 rgba(0, 0, 0, 0.06);
        }

        .summary-card h3 {
            font-size: 0.875rem;
            font-weight: 500;
            color: hsl(var(--muted-foreground));
            margin: 0 0 0.5rem 0;
            text-transform: uppercase;
            letter-spacing: 0.025em;
        }

        .summary-card .value {
            font-size: 2rem;
            font-weight: 700;
            color: hsl(var(--foreground));
            margin: 0;
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
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>bozobackup Report</h1>`)
}

// writeSummaryCards writes the summary statistics cards
func writeSummaryCards(f *os.File, summary AccountingSummary, totalTime time.Duration) {
	f.WriteString(`        <div class="summary-grid">`)

	// Total Files
	f.WriteString(fmt.Sprintf(`
            <div class="summary-card">
                <h3>Total Files</h3>
                <p class="value">%d</p>
            </div>`, summary.TotalFiles))

	// Files Copied
	f.WriteString(fmt.Sprintf(`
            <div class="summary-card">
                <h3>Copied</h3>
                <p class="value">%d</p>
            </div>`, summary.Copied))

	// Duplicates
	f.WriteString(fmt.Sprintf(`
            <div class="summary-card">
                <h3>Duplicates</h3>
                <p class="value">%d</p>
            </div>`, summary.Duplicates))

	// Skipped
	f.WriteString(fmt.Sprintf(`
            <div class="summary-card">
                <h3>Skipped</h3>
                <p class="value">%d</p>
            </div>`, summary.Skipped))

	// Errors
	f.WriteString(fmt.Sprintf(`
            <div class="summary-card">
                <h3>Errors</h3>
                <p class="value">%d</p>
            </div>`, summary.Errors))

	// Total Size
	f.WriteString(fmt.Sprintf(`
            <div class="summary-card">
                <h3>Size Copied</h3>
                <p class="value">%s</p>
            </div>`, formatFileSize(summary.TotalBytes)))

	// Time Taken
	f.WriteString(fmt.Sprintf(`
            <div class="summary-card">
                <h3>Time Taken</h3>
                <p class="value">%s</p>
            </div>`, totalTime.Round(time.Second)))

	f.WriteString(`        </div>`)
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

	f.WriteString(fmt.Sprintf(`
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
		escapedDetails))
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

	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// writeJavaScript writes the JavaScript for search, filter, and sort functionality
func writeJavaScript(f *os.File) {
	f.WriteString(`
        <script>
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
        </script>
    </div>`)
}
