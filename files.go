// bozobackup: Incremental, deduplicating photo/video backup tool with HTML reporting.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
)

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
