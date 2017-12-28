package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"net/http"
	"path/filepath"

	"github.com/sahilm/fuzzy"
)

const (
	IMAGE       string = "image"
	VIDEO       string = "video"
	AUDIO       string = "audio"
	TEXT        string = "text"
	OTHER       string = "other" // zip/rar
	UNSUPPORTED string = "unsupported"
)

type MetaData struct {
	Description string
	MediaType   string
	Tags        []string
	People      []string
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
	Hash string
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

// The DB where files are stored.
type FileDB struct {
	// file UUID key, File object value
	Data         map[string]File
	Transactions []Transaction
	dir          string
	file         string
	requestPool  chan FileAccessRequest
}

// Initialise FileDB by populating from gob file.
func NewFileDB(dbDir string) (fileDB *FileDB, err error) {
	// check db/temp directory exists
	if err = EnsureDirExists(dbDir + "/temp/"); err != nil {
		return
	}

	// init file DB
	fileDB = &FileDB{Data: make(map[string]File), dir: dbDir, file: dbDir + "/file_db.dat"}
	err = fileDB.deserializeFromFile()

	// start request poller
	go fileDB.StartFileAccessPoller()

	return
}

// Structure for passing request and response data between poller.
type FileAccessRequest struct {
	stringOut    chan string
	errorOut     chan error
	filesOut     chan []File
	stringsOut   chan []string
	operation    string
	fileParam    string
	target       string
	searchParams SearchRequest
	fileMetadata MetaData
}

// Poll for requests, process them & pass result/error back to requester via channels.
func (db *FileDB) StartFileAccessPoller() {
	db.requestPool = make(chan FileAccessRequest)

	for req := range db.requestPool {
		// process request
		switch req.operation {
		case "storeFile":
			req.errorOut <- db.storeFile(req.fileParam, req.fileMetadata)
			db.serializeToFile()
		case "deleteFile":
			req.errorOut <- db.deleteFile(req.fileParam, false)
			db.serializeToFile()
		case "getMetaData":
			req.stringsOut <- db.getMetaData(req.target)
		case "search":
			req.filesOut <- db.search(req.searchParams)
		case "serialize":
			req.errorOut <- db.serializeToFile()
		case "deserialize":
			req.errorOut <- db.deserializeFromFile()
		case "destroy":
			req.errorOut <- db.destroy()
		default:
			req.errorOut <- fmt.Errorf("unsupported file access operation")
		}
	}
}

// Create transaction and add to DB.
func (db *FileDB) recordTransaction(transactionType TransactionType, targetFileUUID string) {
	newTransaction := Transaction{UUID: NewUUID(), CreationTimestamp: time.Now().Unix(), Type: transactionType, TargetFileUUID: targetFileUUID, Version: config.params["version"].val}
	db.Transactions = append(db.Transactions, newTransaction)
}

// Check if a file exists in the DB with the specified file UUID.
func (db *FileDB) fileExists(fileUUID string) bool {
	_, ok := db.Data[fileUUID]
	return ok
}

// Upload file to temp dir in a subdir named as the UUID of the session user.
func (db *FileDB) uploadFileToTemp(w http.ResponseWriter, r *http.Request, user User) (filePath, fileName string, err error) {
	// check form file
	newFormFile, handler, err := r.FormFile("file-input")
	if err != nil {
		return
	}
	defer newFormFile.Close()

	// if a temp file for the user does not exist, create one named by their UUID
	tempFilePath := config.rootPath + "/db/temp/" + user.UUID + "/"
	if err = EnsureDirExists(tempFilePath); err != nil {
		return
	}

	// check if a file already exists with the same name in temp dir
	ok, err := FileOrDirExists(tempFilePath + handler.Filename)
	if err != nil {
		return
	}
	if ok {
		err = fmt.Errorf("already_stored")
		return
	}

	// create new empty file
	tempFile, err := os.OpenFile(tempFilePath+handler.Filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return
	}
	defer tempFile.Close()

	// copy file from form to new local temp file
	if _, err = io.Copy(tempFile, newFormFile); err != nil {
		return
	}

	// validate file name/extension
	_, extension := SplitFileName(handler.Filename)
	if fileType := config.CheckMediaType(extension); fileType == UNSUPPORTED {
		err =  fmt.Errorf("format_not_supported")
		os.Remove(tempFilePath + handler.Filename) // delete temp file if unrecognised extension
		return
	}

	// generate hash of file contents & compare against the hashes of files stored in DB
	newHash, err := GenerateFileHash(tempFilePath + handler.Filename)
	if err != nil {
		os.Remove(tempFilePath + handler.Filename) // delete temp file if already exists in DB
		return
	}
	for _, file := range db.Data {
		if file.Hash == newHash {
			err = fmt.Errorf("already_stored")
			os.Remove(tempFilePath + handler.Filename) // delete temp file if already exists in DB
			return
		}
	}

	return "/temp_uploaded/" + user.UUID + "/", handler.Filename, nil
}

// Add a file to the DB.
func (db *FileDB) storeFile(tempFilePath string, metaData MetaData) (err error) {
	// create new file Data struct
	newFile := File{AddedTimestamp: time.Now().Unix(), State: OK, MetaData: metaData, UUID: NewUUID()}

	// validate file name/extension
	newFile.Name, newFile.Extension = SplitFileName(filepath.Base(tempFilePath))
	if len(newFile.Name) == 0 || len(newFile.Extension) == 0 {
		return fmt.Errorf("invalid_file")
	}
	if newFile.MetaData.MediaType = config.CheckMediaType(newFile.Extension); newFile.MetaData.MediaType == UNSUPPORTED {
		os.Remove(tempFilePath) // destroy temp file on add failure
		return err
	}

	// move file from db/temp dir to db/content dir
	if err = os.Rename(tempFilePath, newFile.AbsolutePath()); err != nil {
		os.Remove(tempFilePath) // destroy temp file on add failure
		return err
	}

	// generate hash of file contents
	if newFile.Hash, err = GenerateFileHash(newFile.AbsolutePath()); err != nil {
		os.Remove(newFile.AbsolutePath()) // destroy file on add failure
		return err
	}

	db.Data[newFile.UUID] = newFile
	// record transaction
	db.recordTransaction(CREATE, newFile.UUID)

	return nil
}

// Get specific DB related metadata.
func (db *FileDB) getMetaData(target string) (result []string) {
	resultMap := make(map[string]bool)

	// min/max dates data request
	if target == "dates" {
		sortedFiles := SortFilesByDate(db.toSlice())
		if len(sortedFiles) > 0 {
			minDate := fmt.Sprintf("%d", sortedFiles[len(sortedFiles)-1].AddedTimestamp)
			maxDate := fmt.Sprintf("%d", sortedFiles[0].AddedTimestamp)
			result = append(result, minDate)
			result = append(result, maxDate)
		}
		return
	}

	// other data request types
	for _, file := range db.Data {
		switch target {
		case "tags":
			for _, tag := range file.Tags {
				resultMap[tag] = true
			}
		case "people":
			for _, person := range file.People {
				resultMap[person] = true
			}
		case "file_types":
			resultMap[strings.Title(file.MediaType)] = true
		}
	}

	// map to slice
	for item := range resultMap {
		result = append(result, item)
	}

	return
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

	db.recordTransaction(DELETE, file.UUID)

	return nil
}

// Get a filtered JSON representation of the File properties.
func FilesToJSON(files []File, pretty bool) string {
	// jsonify
	jsonBuffer := &bytes.Buffer{}
	encoder := json.NewEncoder(jsonBuffer)
	encoder.SetEscapeHTML(false)

	if err := encoder.Encode(files); err != nil {
		return err.Error()
	}

	// pretty print
	if pretty {
		indentBuffer := &bytes.Buffer{}
		if err := json.Indent(indentBuffer, jsonBuffer.Bytes(), "", "\t"); err != nil {
			return string(jsonBuffer.Bytes())
		}
		jsonBuffer = indentBuffer
	}

	return string(jsonBuffer.Bytes())
}

// Sort a list of Files by date.
func SortFilesByDate(files []File) []File {
	sort.Slice(files, func(i, j int) bool {
		return files[i].AddedTimestamp > files[j].AddedTimestamp
	})
	return files
}

// Search the DB for Files which match the provided criteria.
func (db *FileDB) search(searchReq SearchRequest) []File {
	files := db.toSlice()
	var filterResults, searchResults []File

	// fuzzy search by description
	if searchReq.description != "" {
		// create a slice of descriptions
		descriptionData := make([]string, len(db.Data))
		for i, file := range files {
			descriptionData[i] = file.Description
		}

		// fuzzy search description for matches
		matches := fuzzy.Find(searchReq.description, descriptionData)
		searchResults = make([]File, len(matches))

		for i, match := range matches {
			searchResults[i] = files[match.Index]
		}

	} else {
		// if no description search criteria was supplied, then specific order does not matter - sort results date descending
		searchResults = SortFilesByDate(files)
	}

	// false = add file to results, true = remove file from results
	ignoreFiles := make([]bool, len(searchResults))
	keepCounter := 0

	for i := range searchResults {
		// trim HH:MM:SS to filter by year/month/day only
		minSearchDate := TrimUnixEpoch(searchReq.minDate)
		maxSearchDate := TrimUnixEpoch(searchReq.maxDate)
		fileDate := TrimUnixEpoch(searchResults[i].AddedTimestamp)

		// min date
		if fileDate.Before(minSearchDate) {
			ignoreFiles[i] = true
			continue
		}
		// max date
		if searchReq.maxDate != 0 && fileDate.After(maxSearchDate) {
			ignoreFiles[i] = true
			continue
		}

		// filter by tags
		if len(searchReq.tags) > 0 {
			tagsMatched := 0
			concatFileTags := "|" + strings.Join(searchResults[i].Tags, "|") + "|"
			// iterate over search request tags checking if they are a substring of the combined file tags
			for _, tag := range searchReq.tags {
				if strings.Contains(concatFileTags, "|"+tag+"|") {
					tagsMatched++
				}
			}
			// tag not found on file
			if tagsMatched < len(searchReq.tags) {
				ignoreFiles[i] = true
				continue
			}
		}

		// filter by people
		if len(searchReq.people) > 0 {
			peopleMatched := 0
			concatFilePeople := "|" + strings.Join(searchResults[i].People, "|") + "|"
			// iterate over search request people checking if they are a substring of the combined file people
			for _, person := range searchReq.people {
				if strings.Contains(concatFilePeople, "|"+person+"|") {
					peopleMatched++
				}
			}
			// tag not found on file
			if peopleMatched < len(searchReq.people) {
				ignoreFiles[i] = true
				continue
			}
		}

		// filter by file types
		if len(searchReq.fileTypes) > 0 {
			typeMatched := false
			// check each search request file type against current file file type
			for _, fileType := range searchReq.fileTypes {
				if fileType == searchResults[i].MediaType {
					typeMatched = true
					break
				}
			}

			// tag not found on file
			if typeMatched == false {
				ignoreFiles[i] = true
				continue
			}
		}

		// increment counter if file is to be kept
		if ignoreFiles[i] == false {
			keepCounter++
		}
	}

	// construct new File slice of selected results
	filterResults = make([]File, keepCounter)
	currentFilterResult := 0
	for i := range searchResults {
		if ignoreFiles[i] == false {
			filterResults[currentFilterResult] = searchResults[i]
			currentFilterResult++
		}
	}

	return filterResults
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
func (db *FileDB) toSlice() (files []File) {
	files = make([]File, 0, len(db.Data))

	// generate slice from Data map
	for _, file := range db.Data {
		if file.State != DELETED {
			files = append(files, file)
		}
	}

	return files
}

// Serialize store map & transactions slice to a specified file.
func (db *FileDB) serializeToFile() (err error) {
	// create/truncate file for writing to
	file, err := os.Create(db.file)
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
	if _, err := os.Stat(db.file); os.IsNotExist(err) {
		db.serializeToFile()
		return nil
	}

	// open file to read from
	file, err := os.Open(db.file)
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

// Delete DB files and reset File DB.
func (db *FileDB) destroy() (err error) {
	err = os.Remove(db.file)
	if err != nil {
		return
	}

	// delete all content files
	RemoveDirContents(config.rootPath + "/static/content/")
	RemoveDirContents(db.dir + "/temp/")

	// reinitialise DB
	db.Data = make(map[string]File)
	db.Transactions = make([]Transaction, 0, 0)
	db.requestPool = make(chan FileAccessRequest)

	log.Println("DB has been reset.")
	return nil
}
