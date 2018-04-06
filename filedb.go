package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

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
type State int

const (
	UPLOADED State = iota
	PUBLISHED
	DELETED
)

// Represents a file's details and its state.
type File struct {
	Name               string
	Extension          string
	UploadedTimestamp  int64
	PublishedTimestamp int64
	State
	MetaData
	Size             int64
	UUID             string
	Hash             string
	UploaderUsername string
}

// Get full absolute path to file.
func (f *File) AbsolutePath() string {
	if f.State == UPLOADED {
		return config.rootPath + "/db/temp/" + f.UploaderUsername + "/" + f.UUID + "." + f.Extension
	}
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
	PublishedFiles map[string]File // viewable by all users
	UploadedFiles  map[string]File // in temp dir, viewable by the uploader only
	Transactions   []Transaction
	dir            string
	file           string
	requestPool    chan FileAccessRequest
}

// Initialise FileDB by populating from gob file.
func NewFileDB(dbDir string) (fileDB *FileDB, err error) {
	// check db/temp directory exists
	if err = EnsureDirExists(dbDir + "/temp/"); err != nil {
		return
	}

	// init file DB
	fileDB = &FileDB{PublishedFiles: make(map[string]File), UploadedFiles: make(map[string]File), dir: dbDir, file: dbDir + "/file_db.dat", requestPool: make(chan FileAccessRequest)}
	err = fileDB.deserializeFromFile()

	// start request poller
	go fileDB.startAccessPoller()

	return
}

// Structure for passing request and response data between poller.
type FileAccessRequest struct {
	operation    string
	UUID         string
	target       string
	state        State
	searchParams SearchRequest
	fileMetadata MetaData
	w            http.ResponseWriter
	r            *http.Request
	user         User
	response     chan FileAccessResponse
}
type FileAccessResponse struct {
	err        error
	file       File
	files      []File
	fileResult FileSearchResult
	metaData   []string
}
type FileSearchResult struct {
	ResultCount int    `json:"result_count"`
	TotalCount  int    `json:"total_count"`
	Files       []File `json:"memories"`
	state       string `json:"-"`
}

// Create a blocking access request and provide an access response.
func (db *FileDB) performAccessRequest(request FileAccessRequest) (response FileAccessResponse) {
	request.response = make(chan FileAccessResponse, 1)
	db.requestPool <- request
	return <-request.response
}

// Poll for requests, process them & pass result/error back to requester via channels.
func (db *FileDB) startAccessPoller() {
	for req := range db.requestPool {
		response := FileAccessResponse{}

		// process request
		switch req.operation {
		case "uploadFile":
			response.file, response.err = db.uploadFile(req.w, req.r, req.user)
			db.serializeToFile()

		case "publishFile":
			response.err = db.publishFile(req.UUID, req.fileMetadata)
			db.serializeToFile()

		case "deleteFile":
			response.err = db.deleteFile(req.UUID)
			db.serializeToFile()

		case "getMetaData":
			response.metaData = db.getMetaData(req.target)

		case "search":
			response.fileResult = db.search(req.searchParams)

		case "getFile":
			response.file = db.PublishedFiles[req.target]

		case "getFilesByUser":
			response.files = db.getFilesByUser(req.UUID, req.state)

		case "serialize":
			response.err = db.serializeToFile()

		case "deserialize":
			response.err = db.deserializeFromFile()

		case "destroy":
			response.err = db.destroy()

		default:
			response.err = fmt.Errorf("unsupported file access operation")
		}

		req.response <- response
	}
}

// Create transaction and add to DB.
func (db *FileDB) recordTransaction(transactionType TransactionType, targetFileUUID string) {
	newTransaction := Transaction{UUID: NewUUID(), CreationTimestamp: time.Now().Unix(), Type: transactionType, TargetFileUUID: targetFileUUID, Version: config.get("version")}
	db.Transactions = append(db.Transactions, newTransaction)
}

// Upload file to temp dir in a subdir named as the UUID of the session user.
func (db *FileDB) uploadFile(w http.ResponseWriter, r *http.Request, user User) (newTempFile File, err error) {
	//
	time.Sleep(time.Millisecond * 100)

	// check form file
	newFormFile, handler, err := r.FormFile("file-input")
	if err != nil {
		config.Log(err.Error(), 2)
		err = fmt.Errorf("error")
		return
	}
	defer newFormFile.Close()

	// if a temp file for the user does not exist, create one named by their UUID
	tempFilePath := config.rootPath + "/db/temp/" + user.Username + "/"
	if err = EnsureDirExists(tempFilePath); err != nil {
		config.Log(err.Error(), 1)
		err = fmt.Errorf("error")
		return
	}

	// create new file object
	newTempFile = File{UploadedTimestamp: time.Now().UnixNano(), State: UPLOADED, UUID: NewUUID(), UploaderUsername: user.Username}

	// separate & validate file name/extension
	newTempFile.Name, newTempFile.Extension = SplitFileName(handler.Filename)
	if newTempFile.Name == "" || newTempFile.Extension == "" {
		err = fmt.Errorf("invalid_file")
		return
	}
	if newTempFile.MediaType = config.CheckMediaType(newTempFile.Extension); newTempFile.MediaType == UNSUPPORTED {
		err = fmt.Errorf("format_not_supported")
		return
	}

	// create new empty file
	tempFile, err := os.OpenFile(newTempFile.AbsolutePath(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		config.Log(err.Error(), 1)
		err = fmt.Errorf("error")
		return
	}
	defer tempFile.Close()

	// copy file from form to new local temp file (must from now on delete file if a failure occurs after copy)
	if _, err = io.Copy(tempFile, newFormFile); err != nil {
		config.Log(err.Error(), 1)
		err = fmt.Errorf("error")
		return
	}

	// get file size
	fileStat, err := tempFile.Stat()
	if err != nil {
		err = fmt.Errorf("error")
		os.Remove(newTempFile.AbsolutePath()) // delete temp file on error
		return
	}
	newTempFile.Size = fileStat.Size()

	// generate hash of file contents
	newTempFile.Hash, err = GenerateFileHash(newTempFile.AbsolutePath())
	if err != nil {
		config.Log(err.Error(), 1)
		err = fmt.Errorf("error")
		os.Remove(newTempFile.AbsolutePath()) // delete temp file on error
		return
	}

	// for each below, inform user if they themselves uploaded the original copy of a colliding file:
	// compare hash against the hashes of files stored in published DB
	for _, file := range db.PublishedFiles {
		if file.Hash == newTempFile.Hash {
			if file.UploaderUsername == user.Username {
				err = fmt.Errorf("already_published_self")
			} else {
				err = fmt.Errorf("already_published")
			}
			os.Remove(newTempFile.AbsolutePath()) // delete temp file if already exists in DB
			return
		}
	}
	// compare hash against the hashes of files stored in temp DB
	for _, file := range db.UploadedFiles {
		if file.Hash == newTempFile.Hash {
			if file.UploaderUsername == user.Username {
				err = fmt.Errorf("already_uploaded_self")
			} else {
				err = fmt.Errorf("already_uploaded")
			}

			os.Remove(newTempFile.AbsolutePath()) // delete temp file if already exists in DB
			return
		}
	}

	// add to temp file DB
	db.UploadedFiles[newTempFile.UUID] = newTempFile

	return newTempFile, nil
}

// Add a file to the DB.
func (db *FileDB) publishFile(fileUUID string, metaData MetaData) (err error) {
	// append new details to file object
	uploadedFile, ok := db.UploadedFiles[fileUUID]
	if !ok {
		return fmt.Errorf("file_not_found")
	}

	uploadedFile.PublishedTimestamp = time.Now().UnixNano()
	// get MediaType from temp uploaded file object
	metaData.MediaType = uploadedFile.MediaType
	uploadedFile.MetaData = metaData

	// set state to published - causes AbsolutePath to return new static location instead of temp location
	tempFilePath := uploadedFile.AbsolutePath()
	uploadedFile.State = PUBLISHED

	// delete from temp DB
	delete(db.UploadedFiles, fileUUID)

	if err = MoveFile(tempFilePath, uploadedFile.AbsolutePath()); err != nil {
		os.Remove(tempFilePath) // destroy temp file on add failure
		config.Log(err.Error(), 1)
		return fmt.Errorf("file_processing_error")
	}

	// add to file DB & record transaction
	db.PublishedFiles[fileUUID] = uploadedFile
	db.recordTransaction(CREATE, fileUUID)

	return nil
}

// Get specific DB related metadata.
func (db *FileDB) getMetaData(target string) (result []string) {
	resultMap := make(map[string]bool)

	// min/max dates data request
	if target == "dates" {
		sortedFiles := SortFilesByDate(db.toSlice())
		if len(sortedFiles) > 0 {
			minDate := fmt.Sprintf("%d", sortedFiles[len(sortedFiles)-1].PublishedTimestamp)
			maxDate := fmt.Sprintf("%d", sortedFiles[0].PublishedTimestamp)
			result = append(result, minDate)
			result = append(result, maxDate)
		}
		return
	}

	// other data request types
	for _, file := range db.PublishedFiles {
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
func (db *FileDB) deleteFile(fileUUID string) (err error) {
	// check if file exists in either published or temp/uploaded DB
	file, ok := db.UploadedFiles[fileUUID]
	if !ok {
		file, ok = db.PublishedFiles[fileUUID]
		if !ok {
			return fmt.Errorf("file_not_found")
		}
	}

	// set state to deleted (so that other servers will hide the file also)
	switch file.State {
	case UPLOADED:
		if err = os.Remove(file.AbsolutePath()); err != nil {
			config.Log(err.Error(), 1)
			return fmt.Errorf("file_processing_error")
		}
		delete(db.UploadedFiles, fileUUID)

	case PUBLISHED:
		file.State = DELETED
		db.PublishedFiles[fileUUID] = file
		db.recordTransaction(DELETE, file.UUID)

	case DELETED:
		return fmt.Errorf("file_already_deleted")
	}

	return nil
}

// Get a filtered JSON representation of the File properties.
func ToJSON(obj interface{}, pretty bool) string {
	// jsonify
	jsonBuffer := &bytes.Buffer{}
	encoder := json.NewEncoder(jsonBuffer)
	encoder.SetEscapeHTML(false)

	if err := encoder.Encode(obj); err != nil {
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
		if files[i].State == UPLOADED {
			return files[i].UploadedTimestamp > files[j].UploadedTimestamp
		}
		return files[i].PublishedTimestamp > files[j].PublishedTimestamp
	})
	return files
}

// Search the DB for Files which match the provided criteria.
func (db *FileDB) search(searchReq SearchRequest) FileSearchResult {
	files := db.toSlice()
	var filterResults, searchResults []File

	// fuzzy search by description
	if searchReq.description != "" {
		// create a slice of descriptions
		descriptionFiles := make([]string, len(db.PublishedFiles))
		for i, file := range files {
			descriptionFiles[i] = file.Description
		}

		// fuzzy search description for matches
		matches := fuzzy.Find(searchReq.description, descriptionFiles)
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
		// trim epoch to HH:MM:SS to filter by year/month/day only
		minSearchDate := TrimUnixEpoch(searchReq.minDate, false)
		maxSearchDate := TrimUnixEpoch(searchReq.maxDate, false)
		fileDate := TrimUnixEpoch(searchResults[i].PublishedTimestamp, true)

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

	// limit number of results to pagination fields
	state := "ok"
	if searchReq.resultsPerPage > 0 {
		rangeBounds := [2]int64{searchReq.page * searchReq.resultsPerPage, (searchReq.page + 1) * searchReq.resultsPerPage}

		if rangeBounds[0] > int64(len(filterResults)-1) {
			// request out of range, return empty result set
			return FileSearchResult{Files: make([]File, 0), ResultCount: 0, TotalCount: len(db.PublishedFiles), state: "empty_results"}
		}
		if rangeBounds[1] > int64(len(filterResults)-1) {
			rangeBounds[1] = int64(len(filterResults))
			state = "end_of_results"
		}

		filterResults = filterResults[rangeBounds[0]:rangeBounds[1]]
	}

	return FileSearchResult{Files: filterResults, ResultCount: len(filterResults), TotalCount: len(db.PublishedFiles), state: state}
}

// Get all files corresponding to User UUID.
func (db *FileDB) getFilesByUser(username string, state State) (files []File) {
	// get uploaded/temp files only
	if state == UPLOADED {
		for _, file := range db.UploadedFiles {
			if file.UploaderUsername == username {
				files = append(files, file)
			}
		}
		return SortFilesByDate(files)
	}

	// get published files only
	if state == PUBLISHED {
		for _, file := range db.PublishedFiles {
			if file.UploaderUsername == username {
				files = append(files, file)
			}
		}
	}

	return SortFilesByDate(files)
}

// Generate slice representation of file PublishedFiles map.
func (db *FileDB) toSlice() (files []File) {
	files = make([]File, 0, len(db.PublishedFiles))

	// generate slice from PublishedFiles map
	for _, file := range db.PublishedFiles {
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
	if err = os.Remove(db.file); err != nil {
		return
	}

	// delete all content files
	RemoveDirContents(config.rootPath + "/static/content/")
	RemoveDirContents(db.dir + "/temp/")

	// reinitialise DB
	db.PublishedFiles = make(map[string]File)
	db.Transactions = make([]Transaction, 0, 0)
	db.requestPool = make(chan FileAccessRequest)

	log.Println("DB has been reset.")
	return nil
}
