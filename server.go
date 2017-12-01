package main

import (
	"log"
	"time"
)

// A server providing file sharing and access related services.
type Server struct {
	startTimestamp int64
	fileDB         *FileDB
}

// Initialise a new file server.
func NewServer(rootPath string) (server *Server, err error) {
	fileDB, err := NewFileDB(rootPath + "/db")
	if err != nil {
		log.Printf("Server error: %v", err.Error())
		return
	}

	// load server config
	server = &Server{fileDB: fileDB, startTimestamp: time.Now().Unix()}
	config.LoadConfig(rootPath)

	fileDB.AddFile(rootPath + "/test/test_1.png")

	//fmt.Printf("%#v \n", fileDB.data)
	//fmt.Println(config.params["version"])

	// start hosting HTTP server to access local file DB (via web UI, with authentication)

	// get a list of other currently online servers providing file updates (via C&C web server) + from local config file containing previously known servers/manually added servers

	// provide these servers with a log of currently owned file hashes, requesting for files we do not own (everyone must have a complete log of all operations)

	// retrieve all files we do not own from the server (one request at a time) + retrieve remote files marked for deletion also, and delete those locally

	// once file DB is up to date, start hosting files to remote servers

	return
}
