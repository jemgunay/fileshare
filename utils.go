package memoryshare

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/twinj/uuid"
	"golang.org/x/crypto/ssh/terminal"
)

// ToJSON returns a JSON representation of any object with the option to pretty print the result.
func ToJSON(obj interface{}, pretty bool) string {
	// jsonify
	jsonBuffer := &bytes.Buffer{}
	encoder := json.NewEncoder(jsonBuffer)
	encoder.SetEscapeHTML(false)

	if err := encoder.Encode(obj); err != nil {
		Critical.Log(err)
		return err.Error()
	}

	// pretty print
	if pretty {
		indentBuffer := &bytes.Buffer{}
		if err := json.Indent(indentBuffer, jsonBuffer.Bytes(), "", "\t"); err != nil {
			Critical.Log(err)
			return string(jsonBuffer.Bytes())
		}
		jsonBuffer = indentBuffer
	}

	return string(jsonBuffer.Bytes())
}

// RemoveDirContents deletes all files in a directory.
func RemoveDirContents(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return errors.Wrap(err, "failed to open directory")
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return errors.Wrap(err, "failed to read directory contents")
	}
	for _, name := range names {
		if err = os.RemoveAll(filepath.Join(dir, name)); err != nil {
			return errors.Wrap(err, "failed to delete file in directory")
		}
	}
	return nil
}

// ProcessInputList splits a string into a list by a delimiter, trims white space, removes duplicates & changes the
// case to lower. Useful for validating search & upload tokenfield tags.
func ProcessInputList(list string, delimiter string, toLowerCase bool) (separated []string) {
	uniqueItems := make(map[string]bool)
	for _, item := range strings.Split(list, delimiter) {
		// process each list element
		trimmedItem := strings.TrimSpace(item)
		if trimmedItem != "" {
			if toLowerCase {
				trimmedItem = strings.ToLower(trimmedItem)
			}
			uniqueItems[trimmedItem] = true
		}
	}

	// convert map to slice
	for item := range uniqueItems {
		separated = append(separated, item)
	}
	return
}

// TrimUnixEpoch converts a unix epoch timestamp to YYYY-MM-DD format (trims anything smaller).
func TrimUnixEpoch(epoch int64, nano bool) time.Time {
	var nanoEpoch int64
	if nano {
		nanoEpoch = epoch
		epoch = 0
	}
	dateParsed := time.Unix(epoch, nanoEpoch).Format("2006-01-02")
	timeParsed, err := time.Parse("2006-01-02", dateParsed)
	if err != nil {
		return time.Now()
	}

	return timeParsed
}

// FileOrDirExists checks whether the given file or directory exists or not.
func FileOrDirExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return true, errors.Wrap(err, "failed to determine file existence state")
}

// EnsureDirExists creates a directory if it does not exist.
func EnsureDirExists(paths ...string) error {
	for _, path := range paths {
		result, err := FileOrDirExists(path)
		if err != nil {
			return err
		}
		if result == false {
			// attempt to create
			if err = os.Mkdir(path, 0755); err != nil {
				return errors.Wrapf(err, "failed to create %v directory", path)
			}
		}
	}
	return nil
}

// MoveFile moves a file to a new location (works across different drives, unlike os.Rename).
func MoveFile(src, dst string) (err error) {
	// copy
	if err = CopyFile(src, dst); err != nil {
		return errors.Wrap(err, "failed to copy file")
	}

	// delete src file
	if err = os.Remove(src); err != nil {
		errors.Wrap(err, "failed to remove file")
	}
	return
}

// CopyFile copies a file to a new location (works across drives, unlike os.Rename).
func CopyFile(src, dst string) error {
	// open src file
	in, err := os.Open(src)
	if err != nil {
		return errors.Wrap(err, "failed to open src file")
	}
	defer in.Close()

	// create dst file
	out, err := os.Create(dst)
	if err != nil {
		return errors.Wrap(err, "failed to create dst file")
	}
	defer out.Close()

	// copy from src to dst
	if _, err = io.Copy(out, in); err != nil {
		return errors.Wrap(err, "failed to copy from src file to dst file")
	}
	return nil
}

// NewUUID generates a new Universally Unique Identifier (UUID).
func NewUUID() (UUID string) {
	return uuid.NewV4().String()
}

// SplitFileName splits a file name into its name & extension components.
func SplitFileName(file string) (name, extension string) {
	components := strings.Split(file, ".")
	if len(components) < 2 {
		return
	}

	// an extension is any chars following the last dot
	nameComponents := components[0 : len(components)-1]
	name = strings.Join(nameComponents, "")
	extension = strings.ToLower(strings.Join(components[len(components)-1:], ""))
	return
}

// GenerateFileHash generates the hash of a file's contents.
func GenerateFileHash(file string) (hash string, err error) {
	f, err := os.Open(file)
	if err != nil {
		return "", errors.Wrap(err, "failed to open file")
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", errors.Wrap(err, "failed to write file to hash writer")
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// FormatByteCount formats bytes to a human readable representation.
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

// ReadStdin reads either visible plaintext or hidden password from Stdin.
func ReadStdin(message string, isPassword bool) (response string, err error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf(message)

	if isPassword {
		bytePassword, err := terminal.ReadPassword(int(syscall.Stdin))
		if err != nil {
			return "", errors.Wrap(err, "failed to read password")
		}

		return strings.TrimSpace(string(bytePassword)), nil
	}

	input, err := reader.ReadString('\n')
	if err != nil {
		err = errors.Wrap(err, "failed to read from Stdin")
	}
	return strings.TrimSpace(input), err
}

// RandomInt returns a random int within the specified range.
func RandomInt(min int, max int) int {
	rand.Seed(time.Now().UnixNano())
	return rand.Intn(max-min) + min
}
