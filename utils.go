package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Delete all files in a directory.
func RemoveDirContents(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		err = os.RemoveAll(filepath.Join(dir, name))
		if err != nil {
			return err
		}
	}
	return nil
}

// Split string into list by delimiter, trim white space & remove duplicates.
func ProcessInputList(list string, delimiter string, toLowerCase bool) (separated []string) {
	items := strings.Split(list, delimiter)
	for _, item := range items {
		trimmedItem := strings.TrimSpace(item)
		if trimmedItem != "" {
			if toLowerCase {
				trimmedItem = strings.ToLower(trimmedItem)
			}
			separated = append(separated, trimmedItem)
		}
	}
	return
}

// Convert unix epoch timestamp to YYYY-MM-DD format (trim anything smaller)
func TrimUnixEpoch(epoch int64) time.Time {
	dateParsed := time.Unix(epoch, 0).UTC().Format("2006-01-02")
	timeParsed, err := time.Parse("2006-01-02", dateParsed)
	if err != nil {
		return time.Now()
	}
	return timeParsed
}