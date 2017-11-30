package main

import (
	"encoding/gob"
	"os"
)

// The hash of a file's contents.
type Hash string

// State of a file.
/*	1 - Remote download/local upload completed
	2 - Currently downloading from remote server/uploading from local
	3 - File has been deleted (mark for deletion on other servers also)
*/
type FileState int

// Represents a file and its state.
type File struct {
	Name           string
	Extension      string
	State          FileState
	AddedTimestamp int64
}

// The DB where files are stored.
type FileDB struct {
	// file hash key, File object value
	data    map[Hash]File
	gobPath string
}

// Initialise FileDB by populating from gob file.
func NewFileDB(gobPath string) (fileDB *FileDB, err error) {
	fileDB = &FileDB{data: make(map[Hash]File), gobPath: gobPath}
	err = fileDB.DeserializeFromFile()
	return
}

// Check if a file exists in the DB with the specified file hash.
func (db *FileDB) FileExists(fileHash string) bool {
	_, ok := db.data[Hash(fileHash)]
	return ok
}

// Add a file to the DB.
func (db *FileDB) AddFile() {
	//.AddedTimestamp = time.Now().Unix()
}

// Delete a file from the DB.
func (db *FileDB) DeleteFile() {

}

// Serialize store map to a specified file.
func (db *FileDB) SerializeToFile() error {
	// create/truncate file for writing to
	file, err := os.Create(db.gobPath)
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
func (db *FileDB) DeserializeFromFile() error {
	// check if file exists
	if _, err := os.Stat(db.gobPath); os.IsNotExist(err) {
		return nil
	}

	// open file to read from
	file, err := os.Open(db.gobPath)
	defer file.Close()
	if err != nil {
		return err
	}

	// decode file contents to store map
	decoder := gob.NewDecoder(file)
	err = decoder.Decode(&db.data)
	if err != nil {
		return err
	}

	return nil
}
