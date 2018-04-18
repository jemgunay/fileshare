package memoryshare

import (
	"encoding/gob"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sahilm/fuzzy"
)

const (
	// IMAGE represents an image media file type.
	IMAGE       = "image"
	// VIDEO represents an video media file type.
	VIDEO       = "video"
	// AUDIO represents an audio media file type.
	AUDIO       = "audio"
	// TEXT represents an text media file type.
	TEXT        = "text"
	// OTHER represents any other supported media file type.
	OTHER       = "other" // zip/rar
	// UNSUPPORTED represents an unsupported media file type.
	UNSUPPORTED = "unsupported"
)

// MetaData contains memory orientated data associated with a File.
type MetaData struct {
	Description string
	MediaType   string
	Tags        []string
	People      []string
}

// State represents a file's state.
type State int

const (
	// UPLOADED represents a File which has been privately uploaded but not published.
	UPLOADED State = iota
	// PUBLISHED represents a File which has been published and is publicly viewable by any logged in users. Published
	// Files will have a corresponding Transaction.
	PUBLISHED
	// DELETED represents a published File which has been marked as deleted and which is no longer visible to users.
	// There will be another Transaction corresponding with the deletion.
	DELETED
)

// File contains details about a media file and its corresponding memory metadata.
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

// AbsolutePath determines the full absolute path to file.
func (f *File) AbsolutePath() string {
	if f.State == UPLOADED {
		return config.RootPath + "/db/temp/" + f.UploaderUsername + "/" + f.UUID + "." + f.Extension
	}
	return config.RootPath + "/static/content/" + f.UUID + "." + f.Extension
}

// TransactionType the type of memory transformation operation documented.
type TransactionType int

const (
	CREATE TransactionType = iota
	EDIT
	DELETE
	MERGE
)

// Transaction is an immutable record of a successful FileDB transforming request.
type Transaction struct {
	UUID              string
	TargetFileUUID    string
	Type              TransactionType
	CreationTimestamp int64
	Version           string
}

// TransactionMutex wraps all Transformations to allow permit concurrent access.
type TransactionMutex struct {
	Transactions []Transaction
	mu           sync.Mutex
}

// Create creates a new Transaction and adds it to the Transactions list.
func (tm *TransactionMutex) Create(transactionType TransactionType, fileUUID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	newTransaction := Transaction{
		UUID:              NewUUID(),
		CreationTimestamp: time.Now().Unix(),
		Type:              transactionType,
		TargetFileUUID:    fileUUID,
		Version:           config.Get("version"),
	}
	tm.Transactions = append(tm.Transactions, newTransaction)
}

// FileMapMutex wraps all Files to permit safe concurrent access.
type FileMapMutex struct {
	Files map[string]File
	mu    sync.Mutex
	name  string
}

// Set creates or updates a File in a FileDB.
func (fm *FileMapMutex) Set(UUID string, file File) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.Files[UUID] = file
}

// Get attempts to retrieve a File from a FileDB.
func (fm *FileMapMutex) Get(UUID string) (file File, ok bool) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	file, ok = fm.Files[UUID]
	return
}

// Count returns the number of Files in a FileDB.
func (fm *FileMapMutex) Count() (size int) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	return len(fm.Files)
}

// Delete removes a File from a FileDB.
func (fm *FileMapMutex) Delete(UUID string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	delete(fm.Files, UUID)
}

// FileMapDB is a File container, where the map key is the file UUID.
type FileMapDB map[string]File

// FileMapFunc is used to pass functions and a FileDB identifier string to PerformFunc which allows concurrency safe
// FileDB access.
type FileMapFunc func(FileMapDB, string) interface{}

// PerformFunc executes the FileMapFunc, wrapping it in a Mutex lock to serialise access. This is used for more complex
// operations where many locking and unlocking operations would have been required otherwise.
func (fm *FileMapMutex) PerformFunc(fileMapFunc FileMapFunc) interface{} {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	return fileMapFunc(fm.Files, fm.name)
}

// FileDB is the database where uploaded files, published files and all file related transactions are stored.
type FileDB struct {
	// file UUID key, File object value
	Published        FileMapMutex     // viewable by all users
	Uploaded         FileMapMutex     // in temp dir, viewable by the uploader only
	FileTransactions TransactionMutex // uniquely documents all memory creations/transformations

	dir  string
	file string
}

// LockAll locks all child Mutexes on the FileDB. Used when serializing the entire FileDB to file.
func (db *FileDB) LockAll() {
	db.Uploaded.mu.Lock()
	db.Published.mu.Lock()
	db.FileTransactions.mu.Lock()
}

// UnlockAll unlocks all child Mutexes on the FileDB.
func (db *FileDB) UnlockAll() {
	db.Uploaded.mu.Unlock()
	db.Published.mu.Unlock()
	db.FileTransactions.mu.Unlock()
}

// NewFileDB initialises the FileDB containers and populates them with data from the stored file if possible. Otherwise,
// a new file is created containing the serialized empty FileDB.
func NewFileDB(dbDir string) (fileDB *FileDB, err error) {
	// check db/temp & static/content directories exists
	if err = EnsureDirExists(dbDir+"/temp/", config.RootPath+"/static/content/"); err != nil {
		return
	}

	// init file DB
	fileDB = &FileDB{
		Published:        FileMapMutex{Files: make(FileMapDB), name: "Published"},
		Uploaded:         FileMapMutex{Files: make(FileMapDB), name: "Uploaded"},
		FileTransactions: TransactionMutex{Transactions: make([]Transaction, 0, 0)},
		dir:              dbDir,
		file:             dbDir + "/file_db.dat",
	}

	// store to file
	err = fileDB.deserializeFromFile()
	return
}

// FileSearchResult is a structure for returning File search results from FileDB.search.
type FileSearchResult struct {
	ResultCount int    `json:"result_count"`
	TotalCount  int    `json:"total_count"`
	Files       []File `json:"memories"`
	state       string
}

// uploadFile handler the uploading of files to the temp dir in a subdir named after the username of the session user.
// These files have not yet been published and will only be viewable by the uploader below the upload form.
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
	hashMatch := func(m FileMapDB, mapName string) interface{} {
		for _, file := range m {
			if file.Hash == newTempFile.Hash {
				dbPrefix := "already_published"
				if mapName == "Uploaded" {
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

	// add to temp file DB
	db.Uploaded.Set(newTempFile.UUID, newTempFile)
	db.serializeToFile()

	return newTempFile, nil
}

// publishFile publishes the file, making it visible to all logged in users. User input Metadata is also added to the
// file here and the original temp file will be deleted.
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

// getMetaData returns a specified type of DB related metadata.
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
	uploadedMetadata := func(m FileMapDB, mapName string) interface{} {
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

// deleteFile marks a published file in the DB as deleted, or deletes an actual temp uploaded file.
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

// SortFilesByDate sorts a list of Files by date.
func SortFilesByDate(files []File) []File {
	sort.Slice(files, func(i, j int) bool {
		if files[i].State == UPLOADED {
			return files[i].UploadedTimestamp > files[j].UploadedTimestamp
		}
		return files[i].PublishedTimestamp > files[j].PublishedTimestamp
	})
	return files
}

// search searches the DB for Files which match the specified criteria.
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

// getFilesByUser retrieves all uploaded or published files corresponding to a User's username.
func (db *FileDB) getFilesByUser(username string, state State) (files []File) {
	filesByUser := func(m FileMapDB, mapName string) interface{} {
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

// getRandomFile returns a randomly selected file.
func (db *FileDB) getRandomFile() (File, error) {
	UUIDs := db.getUUIDs()

	if len(UUIDs) == 0 {
		return File{}, fmt.Errorf("no files have been uploaded")
	}

	// pick random from slice
	randomUUID := UUIDs[RandomInt(0, len(UUIDs))]
	file, ok := db.Published.Get(randomUUID)
	if !ok {
		return File{}, fmt.Errorf("error")
	}

	return file, nil
}

// getUUIDs gets all File UUIDs stored in the FileDB.
func (db *FileDB) getUUIDs() []string {
	getUUIDs := func(m FileMapDB, mapName string) interface{} {
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

// toSlice generates slice representations of the published file map.
func (db *FileDB) toSlice() []File {
	// generate slice from Published map
	publishedToSlice := func(m FileMapDB, mapName string) interface{} {
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

// serializeToFile serializes the entire FileDB to a file on disk via gob.
func (db *FileDB) serializeToFile() (err error) {
	db.LockAll()
	defer db.UnlockAll()

	// create/truncate file for writing to
	file, err := os.Create(db.file)
	if err != nil {
		Critical.Log(err)
		return err
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

// deserializeFromFile deserializes a file to the FileDB structure, overwriting current map values.
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

// reset deletes all DB files and resets the FileDB.
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
