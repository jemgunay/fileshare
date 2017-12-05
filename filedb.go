package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// The hash of a file's contents.
type Hash string

// Media type of a file.
type MediaType int

const (
	IMAGE MediaType = iota
	VIDEO
	AUDIO
	TEXT
	OTHER // zip/rar
	UNSUPPORTED
)

type MetaData struct {
	Description string
	MediaType
	Tags   []string
	People []string
}

// State of a file:
/*	0 - OK
	1 - DELETED (mark for deletion on other servers also)
*/
type State int

const (
	OK State = iota
	DELETED
)

// Represents a file and its state.
type File struct {
	Name           string
	Extension      string
	AddedTimestamp int64
	State
	MetaData
}

func (f *File) ConstructWholePath() string {
	return config.rootPath + "/static/content/" + f.Name + "." + f.Extension
}

// The DB where files are stored.
type FileDB struct {
	// file hash key, File object value
	data        map[Hash]File
	dbPath      string
	gobFilePath string
	requestPool chan FileAccessRequest
}

// Initialise FileDB by populating from gob file.
func NewFileDB(dbPath string) (fileDB *FileDB, err error) {
	fileDB = &FileDB{data: make(map[Hash]File), dbPath: dbPath, gobFilePath: dbPath + "/db.dat"}
	err = fileDB.DeserializeFromFile()

	go fileDB.StartFileAccessPoller()

	return
}

// Check if a file exists in the DB with the specified file hash.
func (db *FileDB) FileExists(fileHash Hash) bool {
	_, ok := db.data[fileHash]
	return ok
}

// Add a file to the DB.
func (db *FileDB) AddFile(tempFilePath string) (err error) {
	// create new file data struct
	newFile := File{AddedTimestamp: time.Now().Unix(), State: OK}

	// set extension and file name
	if len(filepath.Ext(tempFilePath)) < 2 {
		return fmt.Errorf("invalid file format")
	}
	newFile.Extension = string([]rune(filepath.Ext(tempFilePath)[1:]))
	fileNameWithExt := []rune(filepath.Base(tempFilePath))
	newFile.Name = string(fileNameWithExt[:len(fileNameWithExt)-len(newFile.Extension)-1])

	// get media type grouping
	newFile.MediaType, err = config.CheckMediaType(newFile.Extension)
	if err != nil {
		return err
	}

	// move file from db/temp dir to db/content dir
	err = os.Rename(tempFilePath, newFile.ConstructWholePath())
	if err != nil {
		return err
	}

	// generate hash of file contents for DB map key
	movedFile, err := os.Open(newFile.ConstructWholePath())
	if err != nil {
		return err
	}
	defer movedFile.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, movedFile); err != nil {
		return err
	}
	db.data[Hash(hash.Sum(nil))] = newFile

	return nil
}

// Mark a file in the DB for deletion, or delete the actual local copy of the file and remove reference from DB in order to redownload.
func (db *FileDB) DeleteFile(fileHash Hash, hardDelete bool) {
	// set state to deleted (so that other servers will hide the file also)
	file := db.data[fileHash]
	file.State = 2
	db.data[fileHash] = file

	// remove all trace of file (locally and in DB) in order to force a redownload
	if hardDelete {
		file := db.data[fileHash]
		os.Remove(file.ConstructWholePath())
		delete(db.data, fileHash)
	}
}

// Required parameters for providing history on file states to other servers.
type FileHistoryRequest struct {
	Hash
	State
}

// Get a list of all file hashes and their states.
func (db *FileDB) GetFileHistory() (fileHistory string, err error) {
	fileHist := make([]FileHistoryRequest, len(db.data))
	i := 0
	for hash, file := range db.data {
		fileHist[i] = FileHistoryRequest{hash, file.State}
		i++
	}

	// encode file history to string
	buf := new(bytes.Buffer)
	encoder := gob.NewEncoder(buf)
	err = encoder.Encode(fileHist)
	if err != nil {
		log.Println(err)
		return "", err
	}
	return buf.String(), nil
}

// Generate slice representation of file data map.
func (db *FileDB) toSlice(sortByDate bool) (files []File) {
	files = make([]File, 0, len(db.data))

	// generate slice from data map
	for _, file := range db.data {
		if file.State != DELETED {
			files = append(files, file)
		}
	}

	// sort by date added
	if sortByDate {
		sort.Slice(files, func(i, j int) bool {
			return files[i].AddedTimestamp > files[j].AddedTimestamp
		})
	}

	return files
}

// Required parameters for providing history on file states to other servers.
type FileHistoryResponse struct {
	Type                   string
	RequesterMissingHashes []string
	ResponderMissingHashes []string
}

// Process the file history of another server and return a gob encoded list of responses.
func (db *FileDB) ProcessFileHistory(fileHistoryGob string) (err error) {
	var fileHist []FileHistoryRequest

	// decode string to file history
	buf := new(bytes.Buffer)
	decoder := gob.NewDecoder(buf)
	err = decoder.Decode(fileHist)
	if err != nil {
		log.Println(err)
		return err
	}

	//response := FileHistoryResponse{}

	// compare against current file data...
	// -> return file data the requesting server does not have
	// -> return a request for file data we do not have

	return nil
}

// Serialize store map to a specified file.
func (db *FileDB) SerializeToFile() (err error) {
	// create/truncate file for writing to
	file, err := os.Create(db.gobFilePath)
	defer file.Close()
	if err != nil {
		return err
	}

	// encode store map to file
	encoder := gob.NewEncoder(file)
	err = encoder.Encode(&db.data)
	if err != nil {
		return err
	}

	return nil
}

// Deserialize from a specified file to the store map, overwriting current map values.
func (db *FileDB) DeserializeFromFile() (err error) {
	// if db file does not exist, create a new one
	if _, err := os.Stat(db.gobFilePath); os.IsNotExist(err) {
		db.SerializeToFile()
		return nil
	}

	// open file to read from
	file, err := os.Open(db.gobFilePath)
	defer file.Close()
	if err != nil {
		return err
	}

	// decode file contents to store map
	decoder := gob.NewDecoder(file)
	if err = decoder.Decode(&db.data); err != nil {
		return err
	}

	return nil
}

// Structure for passing file access requests and responses.
type FileAccessRequest struct {
	stringOut chan string
	errorOut  chan error
	filesOut  chan []File
	operation string
	fileParam string
}

// Poll for requests and process them
func (db *FileDB) StartFileAccessPoller() {
	db.requestPool = make(chan FileAccessRequest)

	for currentReq := range db.requestPool {
		// process request
		switch currentReq.operation {
		case "addFile":
			currentReq.errorOut <- db.AddFile(currentReq.fileParam)
		case "toString":
			currentReq.filesOut <- db.toSlice(true)
		default:
			currentReq.errorOut <- fmt.Errorf("unsupported file access operation")
		}
	}
}
