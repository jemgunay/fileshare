package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/twinj/uuid"
)

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
/*
	OK
	DELETED (mark for deletion on other servers also)
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
	UUID string
}

// Get full absolute path to file.
func (f *File) AbsolutePath() string {
	return config.rootPath + "/static/content/" + f.UUID + "." + f.Extension
}

// The operation a transaction performed.
type TransactionType int

const (
	CREATE TransactionType = iota
	EDIT
	DELETE
)

// An immutable record of a successful DB changing request.
type Transaction struct {
	UUID              string
	TargetFileUUID    string
	Type              TransactionType
	CreationTimestamp int64
	Version           string
}

// Create transaction and add to DB.
func (db *FileDB) CreateTransaction(transactionType TransactionType, targetFileUUID string) {
	newTransaction := Transaction{UUID: uuid.NewV4().String(), CreationTimestamp: time.Now().Unix(), Type: transactionType, TargetFileUUID: targetFileUUID, Version: config.params["version"]}
	db.Transactions = append(db.Transactions, newTransaction)
}

// The DB where files are stored.
type FileDB struct {
	// file UUID key, File object value
	Data         map[string]File
	Transactions []Transaction
	dbPath       string
	filePath     string
	requestPool  chan FileAccessRequest
}

// Initialise FileDB by populating from gob file.
func NewFileDB(dbPath string) (fileDB *FileDB, err error) {
	fileDB = &FileDB{Data: make(map[string]File), dbPath: dbPath, filePath: dbPath + "/db.dat"}
	err = fileDB.deserializeFromFile()

	go fileDB.StartFileAccessPoller()

	return
}

// Check if a file exists in the DB with the specified file UUID.
func (db *FileDB) fileExists(fileUUID string) bool {
	_, ok := db.Data[fileUUID]
	return ok
}

// Add a file to the DB.
func (db *FileDB) addFile(tempFilePath string, metaData MetaData) (err error) {
	// create new file Data struct
	newFile := File{AddedTimestamp: time.Now().Unix(), State: OK, MetaData: metaData}

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

	// generated UUID to use as storage filename
	newFile.UUID = uuid.NewV4().String()

	// move file from db/temp dir to db/content dir
	err = os.Rename(tempFilePath, newFile.AbsolutePath())
	if err != nil {
		return err
	}

	db.Data[newFile.UUID] = newFile

	db.CreateTransaction(CREATE, newFile.UUID)

	return nil
}

// Mark a file in the DB for deletion, or delete the actual local copy of the file and remove reference from DB in order to redownload.
func (db *FileDB) deleteFile(fileUUID string, hardDelete bool) (err error) {
	if db.fileExists(fileUUID) == false {
		return fmt.Errorf("file does not exist")
	}

	// set state to deleted (so that other servers will hide the file also)
	file := db.Data[fileUUID]
	file.State = 2
	db.Data[fileUUID] = file

	// remove all trace of file (locally and in DB) in order to force a redownload
	if hardDelete {
		file := db.Data[fileUUID]
		os.Remove(file.AbsolutePath())
		delete(db.Data, fileUUID)
	}

	db.CreateTransaction(DELETE, file.UUID)

	return nil
}

// Required parameters for providing history on file states to other servers.
type FileHistoryRequest struct {
	UUID string
	State
}

// Get a list of all file UUIDs and their states.
func (db *FileDB) getFileHistory() (fileHistory string, err error) {
	fileHist := make([]FileHistoryRequest, len(db.Data))
	i := 0
	for uuid, file := range db.Data {
		fileHist[i] = FileHistoryRequest{uuid, file.State}
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

// Required parameters for providing history on file states to other servers.
type FileHistoryResponse struct {
	Type                  string
	RequesterMissingUUIDS []string
	ResponderMissingUUIDS []string
}

// Process the file history of another server and return a gob encoded list of responses.
func (db *FileDB) processFileHistory(fileHistoryGob string) (err error) {
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

	// compare against current file Data...
	// -> return file Data the requesting server does not have
	// -> return a request for file Data we do not have

	return nil
}

// Generate slice representation of file Data map.
func (db *FileDB) toSlice(sortByDate bool) (files []File) {
	files = make([]File, 0, len(db.Data))

	// generate slice from Data map
	for _, file := range db.Data {
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

// Serialize store map & transactions slice to a specified file.
func (db *FileDB) serializeToFile() (err error) {
	// create/truncate file for writing to
	file, err := os.Create(db.filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// encode store map to file
	encoder := gob.NewEncoder(file)
	err = encoder.Encode(&db)
	if err != nil {
		return err
	}

	return nil
}

// Deserialize from a specified file to the store map, overwriting current map values.
func (db *FileDB) deserializeFromFile() (err error) {
	// if db file does not exist, create a new one
	if _, err := os.Stat(db.filePath); os.IsNotExist(err) {
		db.serializeToFile()
		return nil
	}

	// open file to read from
	file, err := os.Open(db.filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// decode file contents to store map
	decoder := gob.NewDecoder(file)
	if err = decoder.Decode(&db); err != nil {
		return err
	}

	return nil
}

// Structure for passing request and response data between poller.
type FileAccessRequest struct {
	stringOut    chan string
	errorOut     chan error
	filesOut     chan []File
	operation    string
	fileParam    string
	fileMetadata MetaData
}

// Poll for requests and process them
func (db *FileDB) StartFileAccessPoller() {
	db.requestPool = make(chan FileAccessRequest)

	for req := range db.requestPool {
		// process request
		switch req.operation {
		case "addFile":
			req.errorOut <- db.addFile(req.fileParam, req.fileMetadata)
		case "deleteFile":
			req.errorOut <- db.deleteFile(req.fileParam, false)
		case "serialize":
			req.errorOut <- db.serializeToFile()
		case "deserialize":
			req.errorOut <- db.deserializeFromFile()
		case "toString":
			req.filesOut <- db.toSlice(true)
		default:
			req.errorOut <- fmt.Errorf("unsupported file access operation")
		}
	}
}
