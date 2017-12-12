package main

import (
	"os"
	"path/filepath"
	"strings"
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

// Get the difference in elements between 2 slices.
func GetSliceDifference(a, b []string) []string {
	mb := map[string]bool{}
	for _, x := range b {
		mb[x] = true
	}
	var ab []string
	for _, x := range a {
		if _, ok := mb[x]; !ok {
			ab = append(ab, x)
		}
	}
	return ab
}