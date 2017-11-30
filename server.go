package main

import "log"

// A server providing file sharing and access related services.
type Server struct {
	// server setting functionality (grabbed from config file)
	isCollectingFileUpdates bool
	isProvidingFileUpdates  bool
	isProvidingHTTPRead     bool
	isProvidingHTTPUpload   bool

	configFile string
	fileDB     *FileDB
}

// Initialise a new file server.
func NewServer(configFile string) (server *Server) {
	fileDB, err := NewFileDB(configFile)
	if err != nil {
		log.Printf("Server error: %v", err.Error())
		return
	}

	server = &Server{configFile: configFile, fileDB: fileDB}
	server.LoadConfig()

	// start hosting HTTP server to access local file DB (via web UI)

	// get a list of other currently online servers providing file updates (via C&C web server) + from local config file containing previously known servers/manually added servers

	// provide these servers with a log of currently owned file hashes, requesting for files we do not own (everyone must have a complete log of all operations)

	// retrieve all files we do not own from the server, one request at a time

	return
}

// Load server config from local file.
func (s *Server) LoadConfig() {
	s.isCollectingFileUpdates = false
	s.isProvidingFileUpdates = false
	s.isProvidingHTTPRead = false
	s.isProvidingHTTPUpload = false
}

// Save server config to local file.
func (s *Server) SaveConfig() {

}
