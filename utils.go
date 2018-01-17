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
	"math"
	"bufio"
	"golang.org/x/crypto/ssh/terminal"
	"syscall"
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

// Move a file to a new location (works across drives, unlike os.Rename).
func MoveFile(src, dst string) error {
	// copy
	err := CopyFile(src, dst)
	if err != nil {
		return err
	}

	// delete src file
	return os.Remove(src)
}

// Copy a file to a new location (works across drives, unlike os.Rename).
func CopyFile(src, dst string) error {
	// open src file
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// create dst file
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	// copy from src to dst
	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return nil
}

// Generate new UUID.
func NewUUID() (UUID string) {
	return uuid.NewV4().String()
}

// Split file name into name & extension components.
func SplitFileName(file string) (name, extension string) {
	components := strings.Split(file, ".")
	if len(components) < 2 {
		return
	}

	name = components[0]
	extension = strings.Join(components[1:], "")
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
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// Format bytes to human readable representation.
func FormatByteCount(bytes int64, si bool) string {
	unit := 1000
	pre := "kMGTPE"
	if si {
		unit = 1024
		pre = "KMGTPE"
	}

	// less than KB/KiB
	if bytes < int64(unit) {
		return fmt.Sprintf("%d B", bytes)
	}

	// get corresponding letter from pre
	exp := (int64)(math.Log(float64(bytes)) / math.Log(float64(unit)))
	pre = string([]rune(pre)[exp-1])
	if !si {
		pre += "i"
	}

	// format result
	result := float64(bytes) / math.Pow(float64(unit), float64(exp))
	return fmt.Sprintf("%.1f %sB", result, pre)
}

// Read either plaintext or password from Stdin.
func ReadStdin(message string, isPassword bool) (response string, err error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf(message)

	if isPassword {
		bytePassword, err := terminal.ReadPassword(int(syscall.Stdin))
		if err != nil {
			config.Log(err.Error(), 1)
			return "", err
		}

		return strings.TrimSpace(string(bytePassword)), nil
	}

	input, err := reader.ReadString('\n')
	if err != nil {
		config.Log(err.Error(), 1)
	}
	return strings.TrimSpace(input), err
}