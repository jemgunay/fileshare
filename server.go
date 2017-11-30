package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// A server providing file sharing and access related services.
type Server struct {
	// server settings (grabbed from config file)
	configParams map[string]bool

	configFile     string
	startTimestamp int64
	fileDB         *FileDB
}

// Initialise a new file server.
func NewServer(serverRootPath string) (server *Server) {
	fileDB, err := NewFileDB(serverRootPath + "/config/db")
	if err != nil {
		log.Printf("Server error: %v", err.Error())
		return
	}

	// load server config
	server = &Server{configFile: serverRootPath + "/config/server.conf", fileDB: fileDB}
	server.configParams = make(map[string]bool)
	server.configParams["collect_updates"] = false
	server.configParams["serve_public_updates"] = false
	server.configParams["enable_public_reads"] = false
	server.configParams["enable_public_uploads"] = false
	server.LoadConfig()

	fmt.Println(server.configParams)

	// start hosting HTTP server to access local file DB (via web UI, with authentication)

	// get a list of other currently online servers providing file updates (via C&C web server) + from local config file containing previously known servers/manually added servers

	// provide these servers with a log of currently owned file hashes, requesting for files we do not own (everyone must have a complete log of all operations)

	// retrieve all files we do not own from the server (one request at a time) + retrieve remote files marked for deletion also, and delete those locally

	// once file DB is up to date, start hosting files to remote servers

	server.startTimestamp = time.Now().Unix()
	return
}

// Load server config from local file.
func (s *Server) LoadConfig() (err error) {
	file, err := os.Open(s.configFile)
	if err != nil {
		log.Fatal(err)
		return err
	}
	defer file.Close()

	// read file by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		// check for empty line or # comment
		if strings.TrimSpace(line) == "" || []rune(line)[0] == '#' {
			continue
		}
		// check if param is valid
		paramSplit := strings.Split(line, "=")
		if len(paramSplit) < 2 {
			continue
		}
		for param := range s.configParams {
			if param == paramSplit[0] {
				s.configParams[param] = paramSplit[1] == "true"
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
		return err
	}

	return nil
}

// Save server config to local file.
func (s *Server) SaveConfig() {

}
