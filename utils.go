package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
	"github.com/twinj/uuid"
	"fmt"
	"crypto/sha256"
	"io"
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

// Convert unix epoch timestamp to YYYY-MM-DD format (trim anything smaller).
func TrimUnixEpoch(epoch int64) time.Time {
	dateParsed := time.Unix(epoch, 0).UTC().Format("2006-01-02")
	timeParsed, err := time.Parse("2006-01-02", dateParsed)
	if err != nil {
		return time.Now()
	}
	return timeParsed
}

// Check whether the given file/dir exists or not.
func FileOrDirExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return true, err
}

// If a directory does not exist, create it.
func EnsureDirExists(path string) error {
	result, err := FileOrDirExists(path)
	if err != nil {
		return err
	}
	if result == false {
		// attempt to create
		err = os.Mkdir(path, 0755)
		if err != nil {
			return fmt.Errorf("%v", "failed to create "+path+" directory.")
		}
	}
	return nil
}

// Generate new UUID.
func NewUUID() (UUID string) {
	return uuid.NewV4().String()
}

// Split file name into name & extension.
func SplitFileName(file string) (name, extension string) {
	extensionWithDot := filepath.Ext(file)
	if len(extensionWithDot) < 2 {
		return
	}
	extension = string([]rune(extensionWithDot[1:]))
	fileNameWithExt := []rune(filepath.Base(file))
	upperRange := len(fileNameWithExt)-len(extension)-1
	if upperRange > len(fileNameWithExt) {
		return "", ""
	}
	name = string(fileNameWithExt[:upperRange])
	return
}

// Generate hash of file contents.
func GenerateFileHash(file string) (hash string, err error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return string(h.Sum(nil)), nil
}