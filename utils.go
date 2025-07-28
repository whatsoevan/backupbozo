// bozobackup: Incremental, deduplicating photo/video backup tool with HTML reporting.
package main

import (
	"os/exec"
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
