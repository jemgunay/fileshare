package memoryshare

import (
	"encoding/gob"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sahilm/fuzzy"
	"reflect"
	"sync"
)

const (
	IMAGE       = "image"
	VIDEO       = "video"
	AUDIO       = "audio"
	TEXT        = "text"
	OTHER       = "other" // zip/rar
	UNSUPPORTED = "unsupported"
)

type MetaData struct {
	Description string
	MediaType   string
	Tags        []string
	People      []string
}

// State of a file.
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
	Size               int64
	UUID               string
	Hash               string
	UploaderUsername   string
	State
	MetaData
}

// Get full absolute path to file.
func (f *File) AbsolutePath() string {
	if f.State == UPLOADED {
		return config.RootPath + "/db/temp/" + f.UploaderUsername + "/" + f.UUID + "." + f.Extension
	}
	return config.RootPath + "/static/content/" + f.UUID + "." + f.Extension
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

type TransactionMutex struct {
	Transactions []Transaction
	mu sync.Mutex
}

func (tm *TransactionMutex) Create(transactionType TransactionType, fileUUID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	newTransaction := Transaction{UUID: NewUUID(), CreationTimestamp: time.Now().Unix(), Type: transactionType, TargetFileUUID: fileUUID, Version: config.Get("version")}
	tm.Transactions = append(tm.Transactions, newTransaction)
}

type FileMapMutex struct {
	Files      map[string]File
	mu sync.Mutex
}

func (fm *FileMapMutex) Set(UUID string, file File) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.Files[UUID] = file
}

func (fm *FileMapMutex) Get(UUID string) (file File, ok bool) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	file, ok = fm.Files[UUID]
	return
}

func (fm *FileMapMutex) Count() (size int) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	return len(fm.Files)
}

func (fm *FileMapMutex) Delete(UUID string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	delete(fm.Files, UUID)
}

type MapDB map[string]File
type FileMapFunc func(MapDB) interface{}

func (fm *FileMapMutex) PerformFunc(fileMapFunc FileMapFunc) interface{} {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	return fileMapFunc(fm.Files)
}

// The DB where files are stored.
type FileDB struct {
	// file UUID key, File object value
	Published        FileMapMutex     // viewable by all users
	Uploaded         FileMapMutex     // in temp dir, viewable by the uploader only
	FileTransactions TransactionMutex // uniquely documents all memory creations/transformations

	dir  string
	file string
}

func (db *FileDB) LockAll() {
	db.Uploaded.mu.Lock()
	db.Published.mu.Lock()
	db.FileTransactions.mu.Lock()
}

func (db *FileDB) UnlockAll() {
	db.Uploaded.mu.Unlock()
	db.Published.mu.Unlock()
	db.FileTransactions.mu.Unlock()
}

// Initialise FileDB by populating from gob file.
func NewFileDB(dbDir string) (fileDB *FileDB, err error) {
	// check db/temp & static/content directories exists
	if err = EnsureDirExists(dbDir+"/temp/", config.RootPath+"/static/content/"); err != nil {
		return
	}

	// init file DB
	fileDB = &FileDB{
		Published:        FileMapMutex{Files: make(MapDB)},
		Uploaded:         FileMapMutex{Files: make(MapDB)},
		FileTransactions: TransactionMutex{Transactions: make([]Transaction, 0, 0)},
		dir:              dbDir,
		file:             dbDir + "/file_db.dat",
	}
	err = fileDB.deserializeFromFile()

	return
}

// Structure for passing request and response data between poller.
type FileSearchResult struct {
	ResultCount int    `json:"result_count"`
	TotalCount  int    `json:"total_count"`
	Files       []File `json:"memories"`
	state       string `json:"-"`
}

// Create a blocking access request and provide an access response.
/*func (db *FileDB) performAccessRequest(request FileAccessRequest) (response FileAccessResponse) {
	request.response = make(chan FileAccessResponse, 1)
	db.requestPool <- request
	return <-request.response
}*/

// Poll for requests, process them & pass result/error back to requester via channels.
/*func (db *FileDB) startAccessPoller() {
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
			response.file = db.Published[req.target]

		case "getFilesByUser":
			response.files = db.getFilesByUser(req.UUID, req.state)

		case "getRandomFile":
			response.file = db.getRandomFile()

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
}*/

// Upload file to temp dir in a subdir named as the UUID of the session user.
func (db *FileDB) uploadFile(r *http.Request, user User) (newTempFile File, err error) {
	// check form file
	newFormFile, handler, err := r.FormFile("file-input")
	if err != nil {
		Input.Log(err)
		err = fmt.Errorf("error")
		return
	}
	defer newFormFile.Close()

	// if a temp file for the user does not exist, create one named by their UUID
	tempFilePath := config.RootPath + "/db/temp/" + user.Username + "/"
	if err = EnsureDirExists(tempFilePath); err != nil {
		Critical.Log(err)
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
		Critical.Log(err)
		err = fmt.Errorf("error")
		return
	}
	defer tempFile.Close()

	// copy file from form to new local temp file (must from now on delete file if a failure occurs after copy)
	if _, err = io.Copy(tempFile, newFormFile); err != nil {
		Critical.Log(err)
		err = fmt.Errorf("error")
		return
	}

	// get file size
	fileStat, err := tempFile.Stat()
	if err != nil {
		Critical.Log(err)
		err = fmt.Errorf("error")
		os.Remove(newTempFile.AbsolutePath()) // delete temp file on error
		return
	}
	newTempFile.Size = fileStat.Size()

	// generate hash of file contents
	newTempFile.Hash, err = GenerateFileHash(newTempFile.AbsolutePath())
	if err != nil {
		Critical.Log(err)
		err = fmt.Errorf("error")
		os.Remove(newTempFile.AbsolutePath()) // delete temp file on error
		return
	}

	// for each below, inform user if they themselves uploaded the original copy of a colliding file:
	// compare hash against the hashes of files stored in published DB
	hashMatch := func(m MapDB) interface{} {
		for _, file := range m {
			if file.Hash == newTempFile.Hash {
				dbPrefix := "already_published"
				if reflect.DeepEqual(m, db.Uploaded.Files) {
					dbPrefix = "already_uploaded"
				}

				if file.UploaderUsername == user.Username {
					err = fmt.Errorf(dbPrefix + "_self")
				} else {
					err = fmt.Errorf(dbPrefix)
				}
				os.Remove(newTempFile.AbsolutePath()) // delete temp file if already exists in DB
				return true
			}
		}
		return false
	}

	if db.Published.PerformFunc(hashMatch).(bool) {
		return
	}

	if db.Uploaded.PerformFunc(hashMatch).(bool) {
		return
	}

	/*// compare hash against the hashes of files stored in temp DB
	uploadedHashMatch := func(m MapDB) interface{} {
		for _, file := range m {
			if file.Hash == newTempFile.Hash {
				if file.UploaderUsername == user.Username {
					err = fmt.Errorf("already_uploaded_self")
				} else {
					err = fmt.Errorf("already_uploaded")
				}

				os.Remove(newTempFile.AbsolutePath()) // delete temp file if already exists in DB
				return true
			}
		}
		return false
	}

	if db.Uploaded.PerformFunc(uploadedHashMatch).(bool) {
		return
	}*/

	// add to temp file DB
	db.Uploaded.Set(newTempFile.UUID, newTempFile)
	db.serializeToFile()

	return newTempFile, nil
}

// Add a file to the DB.
func (db *FileDB) publishFile(fileUUID string, metaData MetaData) (err error) {
	// append new details to file object
	uploadedFile, ok := db.Uploaded.Get(fileUUID)
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
	db.Uploaded.Delete(fileUUID)

	if err = MoveFile(tempFilePath, uploadedFile.AbsolutePath()); err != nil {
		os.Remove(tempFilePath) // destroy temp file on add failure
		Critical.Log(err)
		return fmt.Errorf("file_processing_error")
	}

	// add to file DB & record transaction
	db.Published.Set(fileUUID, uploadedFile)
	db.FileTransactions.Create(CREATE, fileUUID)

	db.serializeToFile()
	return nil
}

// Get specific DB related metadata.
func (db *FileDB) getMetaData(target string) (result []string) {
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

	resultMap := make(map[string]bool)

	// other data request types
	uploadedMetadata := func(m MapDB) interface{} {
		for _, file := range m {
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
		return resultMap
	}

	resultMap = db.Published.PerformFunc(uploadedMetadata).(map[string]bool)

	// map to slice
	for item := range resultMap {
		result = append(result, item)
	}
	sort.Strings(result)

	return
}

// Mark a file in the DB for deletion, or delete the actual local copy of the file and remove reference from DB in order to redownload.
func (db *FileDB) deleteFile(fileUUID string) (err error) {
	// check if file exists in either published or temp/uploaded DB
	file, ok := db.Uploaded.Get(fileUUID)
	if !ok {
		file, ok = db.Published.Get(fileUUID)
		if !ok {
			return fmt.Errorf("file_not_found")
		}
	}

	// set state to deleted (so that other servers will hide the file also)
	switch file.State {
	case UPLOADED:
		if err = os.Remove(file.AbsolutePath()); err != nil {
			Critical.Log(err)
			return fmt.Errorf("file_processing_error")
		}
		db.Uploaded.Delete(fileUUID)

	case PUBLISHED:
		file.State = DELETED
		db.Published.Set(fileUUID, file)
		db.FileTransactions.Create(DELETE, file.UUID)

	case DELETED:
		return fmt.Errorf("file_already_deleted")
	}

	db.serializeToFile()
	return nil
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
		descriptionFiles := make([]string, db.Published.Count())
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
	totalCount := db.Published.Count()
	state := "ok"

	if searchReq.resultsPerPage > 0 {
		rangeBounds := [2]int64{searchReq.page * searchReq.resultsPerPage, (searchReq.page + 1) * searchReq.resultsPerPage}

		if rangeBounds[0] > int64(len(filterResults)-1) {
			// request out of range, return empty result set
			return FileSearchResult{Files: make([]File, 0), ResultCount: 0, TotalCount: totalCount, state: "empty_results"}
		}
		if rangeBounds[1] > int64(len(filterResults)-1) {
			rangeBounds[1] = int64(len(filterResults))
			state = "end_of_results"
		}

		filterResults = filterResults[rangeBounds[0]:rangeBounds[1]]
	}

	return FileSearchResult{Files: filterResults, ResultCount: len(filterResults), TotalCount: totalCount, state: state}
}

// Get all files corresponding to User UUID.
func (db *FileDB) getFilesByUser(username string, state State) (files []File) {
	filesByUser := func(m MapDB) interface{} {
		for _, file := range m {
			if file.UploaderUsername == username {
				files = append(files, file)
			}
		}
		return files
	}

	// get uploaded/temp files only
	if state == UPLOADED {
		return SortFilesByDate(db.Uploaded.PerformFunc(filesByUser).([]File))
	}

	// get published files only
	if state == PUBLISHED {
		return SortFilesByDate(db.Published.PerformFunc(filesByUser).([]File))
	}

	return SortFilesByDate(files)
}

// Get a random file.
func (db *FileDB) getRandomFile() (File, error) {
	UUIDs := db.getUUIDs()

	if len(UUIDs) == 0 {
		return File{}, fmt.Errorf("no files have been uploaded")
	}

	// pick random from slice
	randomUUID := UUIDs[randomInt(0, len(UUIDs))]
	file, ok := db.Published.Get(randomUUID)
	if !ok {
		return File{}, fmt.Errorf("error")
	}

	return file, nil
}

func (db *FileDB) getUUIDs() []string {
	getUUIDs := func(m MapDB) interface{} {
		UUIDs := make([]string, len(m))
		counter := 0
		for UUID := range m {
			UUIDs[counter] = UUID
			counter++
		}
		return UUIDs
	}

	return db.Published.PerformFunc(getUUIDs).([]string)
}

// Generate slice representation of file Published map.
func (db *FileDB) toSlice() []File {
	// generate slice from Published map
	publishedToSlice := func(m MapDB) interface{} {
		files := make([]File, 0, len(m))

		for _, file := range m {
			if file.State != DELETED {
				files = append(files, file)
			}
		}

		return files
	}

	return db.Published.PerformFunc(publishedToSlice).([]File)
}

// Serialize store map & transactions slice to a specified file.
func (db *FileDB) serializeToFile() (err error) {
	db.LockAll()
	defer db.UnlockAll()

	// create/truncate file for writing to
	file, err := os.Create(db.file)
	if err != nil {
		return err
		Critical.Log(err)
	}
	defer file.Close()

	// encode & store DB to file
	encoder := gob.NewEncoder(file)

	err = encoder.Encode(&db)
	if err != nil {
		Critical.Log(err)
		return err
	}

	return nil
}

// Deserialize from a specified file to the store map, overwriting current map values.
func (db *FileDB) deserializeFromFile() (err error) {
	db.LockAll()

	// if db file does not exist, create a new one
	if _, err := os.Stat(db.file); os.IsNotExist(err) {
		db.UnlockAll()
		db.serializeToFile()
		return nil
	}
	defer db.UnlockAll()

	// open file to read from
	file, err := os.Open(db.file)
	if err != nil {
		Critical.Log(err)
		return err
	}
	defer file.Close()

	// decode file contents to store map
	decoder := gob.NewDecoder(file)
	if err = decoder.Decode(&db); err != nil {
		Critical.Log(err)
		return err
	}

	return nil
}

// Delete DB files and reset File DB.
func (db *FileDB) reset() (err error) {
	db.LockAll()
	defer db.UnlockAll()
	if err = os.Remove(db.file); err != nil {
		return
	}

	// delete all content files
	RemoveDirContents(config.RootPath + "/static/content/")
	RemoveDirContents(db.dir + "/temp/")

	// reinitialise DB
	db.Published.Files = make(map[string]File)
	db.Uploaded.Files = make(map[string]File)
	db.FileTransactions.Transactions = make([]Transaction, 0, 0)

	Info.Log("DB has been reset.")
	return nil
}
