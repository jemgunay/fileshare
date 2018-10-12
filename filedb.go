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

	"github.com/pkg/errors"
	"github.com/sahilm/fuzzy"
)

const (
	// Image represents an image media file type.
	Image = "image"
	// Video represents an video media file type.
	Video = "video"
	// Audio represents an audio media file type.
	Audio = "audio"
	// Text represents an text media file type.
	Text = "text"
	// Other represents any other supported media file type (i.e. zip or rar).
	Other = "other"
	// Unsupported represents an unsupported media file type.
	Unsupported = "unsupported"
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
	// Uploaded represents a File which has been privately uploaded but not published.
	Uploaded State = iota
	// Published represents a File which has been published and is publicly viewable by any logged in users. Published
	// Files will have a corresponding Transaction.
	Published
	// Deleted represents a published File which has been marked as deleted and which is no longer visible to users.
	// There will be another Transaction corresponding with the deletion.
	Deleted
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
	if f.State == Uploaded {
		return config.rootPath + "/db/temp/" + f.UploaderUsername + "/" + f.UUID + "." + f.Extension
	}
	return config.rootPath + "/static/content/" + f.UUID + "." + f.Extension
}

// TransactionType the type of memory transformation operation documented.
type TransactionType int

const (
	// Create transactions represent a memory creation.
	Create TransactionType = iota
	// Edit transactions represent a memory transformation.
	Edit
	// Delete transactions represent a memory deletion.
	Delete
	// Merge transactions represent a memory merge with a duplicate memory from another service host.
	Merge
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
	mu           sync.RWMutex
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
		Version:           config.Version,
	}
	tm.Transactions = append(tm.Transactions, newTransaction)
}

// FileMapMutex wraps all Files to permit safe concurrent access.
type FileMapMutex struct {
	Files map[string]File
	mu    sync.RWMutex
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
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	file, ok = fm.Files[UUID]
	return
}

// Count returns the number of Files in a FileDB.
func (fm *FileMapMutex) Count() (size int) {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
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
	if err = EnsureDirExists(dbDir, dbDir+"/temp/", config.rootPath+"/static/content/"); err != nil {
		return nil, errors.Wrap(err, "a FileDB directory could not be created")
	}

	// init file DB
	fileDB = &FileDB{
		Published:        FileMapMutex{Files: make(FileMapDB), name: "Published"},
		Uploaded:         FileMapMutex{Files: make(FileMapDB), name: "Uploaded"},
		FileTransactions: TransactionMutex{Transactions: make([]Transaction, 0, 0)},
		dir:              dbDir,
		file:             dbDir + "/file_db.dat",
	}

	// load DB from file
	if err = fileDB.DeserializeFromFile(); err != nil {
		err = errors.Wrap(err, "could not deserialize FileDB from file")
	}
	return
}

// FileSearchResult is a structure for returning File search results from FileDB.search.
type FileSearchResult struct {
	ResultCount int    `json:"result_count"`
	TotalCount  int    `json:"total_count"`
	Files       []File `json:"memories"`
	state       string
}

// ErrInvalidFile implies a file name or extension were invalid.
var ErrInvalidFile = errors.New("invalid file name or extension")

// ErrUnsupportedFormat implies the file is of an unsupported file format.
var ErrUnsupportedFormat = errors.New("unsupported file format")

// FileExistsError implies a file has already been uploaded or published.
type FileExistsError struct {
	state       State
	userIsOwner bool
}

// FileExistsError returns an error message.
func (e *FileExistsError) Error() string {
	return "file already exists in DB"
}

// ConstructResponse constructs the response required by the calling HTTP handler.
func (e *FileExistsError) ConstructResponse() string {
	response := "already_"
	if e.state == Published {
		response += "published"
	} else {
		response += "uploaded"
	}
	if e.userIsOwner {
		response += "_self"
	}
	return response
}

// UploadFile handler the uploading of files to the temp dir in a subdir named after the username of the session user.
// These files have not yet been published and will only be viewable by the uploader below the upload form.
func (db *FileDB) UploadFile(r *http.Request, user User) (newTempFile File, err error) {
	// check form file
	newFormFile, handler, err := r.FormFile("file-input")
	if err != nil {
		err = errors.Wrap(err, "unable to parse file in form")
		return
	}
	defer newFormFile.Close()

	// if a temp dir for the user does not exist, create one named by their UUID
	tempFilePath := config.rootPath + "/db/temp/" + user.Username + "/"
	if err = EnsureDirExists(tempFilePath); err != nil {
		err = errors.Wrap(err, "could not create temp dir for user")
		return
	}

	// create new file object
	newTempFile = File{
		UploadedTimestamp: time.Now().UnixNano(),
		State:             Uploaded,
		UUID:              NewUUID(),
		UploaderUsername:  user.Username,
	}

	// separate & validate file name/extension
	newTempFile.Name, newTempFile.Extension = SplitFileName(handler.Filename)
	if newTempFile.Name == "" || newTempFile.Extension == "" {
		err = ErrInvalidFile
		return
	}
	if newTempFile.MediaType = config.CheckMediaType(newTempFile.Extension); newTempFile.MediaType == Unsupported {
		err = ErrUnsupportedFormat
		return
	}

	// create new empty file
	tempFile, err := os.OpenFile(newTempFile.AbsolutePath(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		err = errors.Wrap(err, "failed to create new upload copy dst file")
		return
	}
	defer tempFile.Close()

	// copy file from form to new local temp file (must from now on delete file if a failure occurs after copy)
	if _, err = io.Copy(tempFile, newFormFile); err != nil {
		err = errors.Wrap(err, "failed to copy new upload to dst file")
		return
	}

	// get file size
	fileStat, err := tempFile.Stat()
	if err != nil {
		err = errors.Wrap(err, "failed to determine file size")
		os.Remove(newTempFile.AbsolutePath()) // delete temp file on error
		return
	}
	newTempFile.Size = fileStat.Size()

	// generate hash of file contents
	newTempFile.Hash, err = GenerateFileHash(newTempFile.AbsolutePath())
	if err != nil {
		err = errors.Wrap(err, "failed to generate hash of file")
		os.Remove(newTempFile.AbsolutePath()) // delete temp file on error
		return
	}

	// for each below, inform user if they themselves uploaded the original copy of a colliding file:
	// compare hash against the hashes of files stored in published DB
	hashMatch := func(m FileMapDB, mapName string) interface{} {
		for _, file := range m {
			if file.Hash == newTempFile.Hash {
				existsErr := &FileExistsError{state: Published, userIsOwner: false}

				if mapName == "Uploaded" {
					existsErr.state = Uploaded
				}
				if file.UploaderUsername == user.Username {
					existsErr.userIsOwner = true
				}

				os.Remove(newTempFile.AbsolutePath()) // delete temp file if already exists in DB
				return existsErr
			}
		}
		return nil
	}

	if hashResult := db.Published.PerformFunc(hashMatch); hashResult != nil {
		return newTempFile, hashResult.(error)
	}
	if hashResult := db.Uploaded.PerformFunc(hashMatch); hashResult != nil {
		return newTempFile, hashResult.(error)
	}

	// add to temp file DB
	db.Uploaded.Set(newTempFile.UUID, newTempFile)
	db.SerializeToFile()

	return newTempFile, nil
}

// ErrFileNotFound implies a file was not found which should exist.
var ErrFileNotFound = errors.New("file not found")

// PublishFile publishes the file, making it visible to all logged in users. User input Metadata is also added to the
// file here and the original temp file will be deleted.
func (db *FileDB) PublishFile(fileUUID string, metaData MetaData) (err error) {
	// append new details to file object
	uploadedFile, ok := db.Uploaded.Get(fileUUID)
	if !ok {
		return ErrFileNotFound
	}

	uploadedFile.PublishedTimestamp = time.Now().UnixNano()
	// get MediaType from temp uploaded file object
	metaData.MediaType = uploadedFile.MediaType
	uploadedFile.MetaData = metaData

	// set state to published - causes AbsolutePath to return new static location instead of temp location
	tempFilePath := uploadedFile.AbsolutePath()
	uploadedFile.State = Published

	// delete from temp DB
	db.Uploaded.Delete(fileUUID)

	if err = MoveFile(tempFilePath, uploadedFile.AbsolutePath()); err != nil {
		os.Remove(tempFilePath) // destroy temp file on add failure
		return errors.Wrap(err, "failed to move temp file to uploads")
	}

	// add to file DB & record transaction
	db.Published.Set(fileUUID, uploadedFile)
	db.FileTransactions.Create(Create, fileUUID)

	db.SerializeToFile()
	return nil
}

// GetMetaData returns a specified type of DB related metadata.
func (db *FileDB) GetMetaData(target string) (result []string) {
	// min/max dates data request
	if target == "dates" {
		sortedFiles := SortFilesByDate(db.ToSlice())
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

// ErrFileAlreadyDeleted implies that the file to be deleted has already been deleted.
var ErrFileAlreadyDeleted = errors.New("file has already been deleted")

// DeleteFile marks a published file in the DB as deleted, or deletes an actual temp uploaded file.
func (db *FileDB) DeleteFile(fileUUID string) (err error) {
	// check if file exists in either published or temp/uploaded DB
	file, ok := db.Uploaded.Get(fileUUID)
	if !ok {
		file, ok = db.Published.Get(fileUUID)
		if !ok {
			return ErrFileNotFound
		}
	}

	// set state to deleted (so that other servers will hide the file also)
	switch file.State {
	case Uploaded:
		if err = os.Remove(file.AbsolutePath()); err != nil {
			return errors.Wrap(err, "target file could not be removed")
		}
		db.Uploaded.Delete(fileUUID)

	case Published:
		file.State = Deleted
		db.Published.Set(fileUUID, file)
		db.FileTransactions.Create(Delete, file.UUID)

	case Deleted:
		return ErrFileAlreadyDeleted
	}

	db.SerializeToFile()
	return nil
}

// SortFilesByDate sorts a list of Files by date.
func SortFilesByDate(files []File) []File {
	sort.Slice(files, func(i, j int) bool {
		if files[i].State == Uploaded {
			return files[i].UploadedTimestamp > files[j].UploadedTimestamp
		}
		return files[i].PublishedTimestamp > files[j].PublishedTimestamp
	})
	return files
}

// Search searches the DB for Files which match the specified criteria.
func (db *FileDB) Search(searchReq SearchRequest) FileSearchResult {
	files := db.ToSlice()
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

// GetFilesByUser retrieves all uploaded or published files corresponding to a User's username.
func (db *FileDB) GetFilesByUser(username string, state State) (files []File) {
	filesByUser := func(m FileMapDB, mapName string) interface{} {
		for _, file := range m {
			if file.UploaderUsername == username {
				files = append(files, file)
			}
		}
		return files
	}

	// get uploaded/temp files only
	if state == Uploaded {
		return SortFilesByDate(db.Uploaded.PerformFunc(filesByUser).([]File))
	}

	// get published files only
	if state == Published {
		return SortFilesByDate(db.Published.PerformFunc(filesByUser).([]File))
	}

	return SortFilesByDate(files)
}

// ErrFileDBEmpty implies that no files have been published to the DB.
var ErrFileDBEmpty = errors.New("no files have been published")

// GetRandomFile returns a randomly selected file.
func (db *FileDB) GetRandomFile() (File, error) {
	UUIDs := db.GetUUIDs()

	if len(UUIDs) == 0 {
		return File{}, ErrFileDBEmpty
	}

	// pick random from slice
	randomUUID := UUIDs[RandomInt(0, len(UUIDs))]
	file, ok := db.Published.Get(randomUUID)
	if !ok {
		return File{}, errors.New("file does not exist")
	}

	return file, nil
}

// GetUUIDs gets all File UUIDs stored in the FileDB.
func (db *FileDB) GetUUIDs() []string {
	accumulateUUID := func(m FileMapDB, mapName string) interface{} {
		UUIDs := make([]string, len(m))
		counter := 0
		for UUID := range m {
			UUIDs[counter] = UUID
			counter++
		}
		return UUIDs
	}

	return db.Published.PerformFunc(accumulateUUID).([]string)
}

// ToSlice generates slice representations of the published file map.
func (db *FileDB) ToSlice() []File {
	// generate slice from Published map
	publishedToSlice := func(m FileMapDB, mapName string) interface{} {
		files := make([]File, 0, len(m))

		for _, file := range m {
			if file.State != Deleted {
				files = append(files, file)
			}
		}

		return files
	}

	return db.Published.PerformFunc(publishedToSlice).([]File)
}

// SerializeToFile serializes the entire FileDB to a file on disk via gob.
func (db *FileDB) SerializeToFile() (err error) {
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
	if err = gob.NewEncoder(file).Encode(&db); err != nil {
		Critical.Log(err)
		return err
	}

	return nil
}

// DeserializeFromFile deserializes a file to the FileDB structure, overwriting current map values.
func (db *FileDB) DeserializeFromFile() (err error) {
	db.LockAll()

	// if db file does not exist, create a new one
	if _, err = os.Stat(db.file); os.IsNotExist(err) {
		db.UnlockAll()
		db.SerializeToFile()
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
	if err = gob.NewDecoder(file).Decode(&db); err != nil {
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
	RemoveDirContents(config.rootPath + "/static/content/")
	RemoveDirContents(db.dir + "/temp/")

	// reinitialise DB
	db.Published.Files = make(map[string]File)
	db.Uploaded.Files = make(map[string]File)
	db.FileTransactions.Transactions = make([]Transaction, 0, 0)

	Info.Log("DB has been reset.")
	return nil
}
