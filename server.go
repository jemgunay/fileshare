package main

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"time"

	"os"

	"io/ioutil"

	"strconv"

	"github.com/gorilla/mux"
)

// A server providing file sharing and access related services.
type ServerBase struct {
	fileDB         *FileDB
	startTimestamp int64
}

// Initialise a new file server.
func NewServerBase() (err error, httpServer HTTPServer) {
	// start hosting HTTP server to access local file DB (via web app UI, with auth)
	// create new file DB
	fileDB, err := NewFileDB(config.rootPath + "/db")
	if err != nil {
		log.Printf("Server error: %v", err.Error())
		return
	}

	// start file manager http server
	if config.params["http_host"] == "" || config.params["http_port"] == "" {
		err = fmt.Errorf("host and port parameters must be specified in config")
		log.Println(err)
		return
	}
	httpPort, err := strconv.Atoi(config.params["http_port"])
	if err != nil {
		err = fmt.Errorf("invalid port found in ")
		log.Println(err)
		return
	}
	httpServer = HTTPServer{host: config.params["http_host"], port: httpPort, ServerBase: ServerBase{fileDB: fileDB, startTimestamp: time.Now().Unix()}}
	httpServer.Start()


	// start hosting files to remote servers

	// get a list of other currently online servers providing file updates (via C&C web server) + from local config file containing previously known servers/manually added servers

	// provide these servers with a log of currently owned file hashes, requesting for files we do not own (everyone must have a complete log of all operations)

	// retrieve all files we do not own from the server (one request at a time) + retrieve remote files marked for deletion also, and delete those locally

	return
}

type HTTPServer struct {
	host string
	port int
	ServerBase
	server *http.Server
}

// Start listening for HTTP requests.
func (s *HTTPServer) Start() {
	// define HTTP routes
	router := mux.NewRouter()

	// view all files & upload form
	router.HandleFunc("/", s.viewFiles).Methods("GET")
	router.HandleFunc("/{file}", s.getFiles).Methods("GET")
	// handle file upload
	router.HandleFunc("/upload/", s.handleUpload).Methods("POST")
	// serve static files
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir(config.rootPath+"/static/"))))

	s.server = &http.Server{
		Handler:      router,
		Addr:         s.host + ":" + strconv.Itoa(s.port),
		WriteTimeout: 5 * time.Second,
		ReadTimeout:  5 * time.Second,
	}

	// listen for HTTP requests
	log.Printf("starting HTTP server on port %d", s.port)

	go func(server *http.Server) {
		// add HTTPS: https://www.kaihag.com/https-and-go/
		if err := server.ListenAndServe(); err != nil {
			log.Println(err)
		}
	}(s.server)
}

// Read single file JSON data.
func (s *HTTPServer) getFiles(w http.ResponseWriter, req *http.Request) {
	s.writeResponse(w, s.ServerBase.fileDB.filesToJSON(), nil)
}

// Process HTTP view files request.
func (s *HTTPServer) viewFiles(w http.ResponseWriter, req *http.Request) {
	fileAR := FileAccessRequest{filesOut: make(chan []File), operation: "toString"}
	s.fileDB.requestPool <- fileAR
	files := <-fileAR.filesOut

	// html template data
	htmlData := struct {
		Title string
		Files []File
	}{
		"Home",
		files,
	}

	// load HTML template from disk
	htmlTemplate, err := ioutil.ReadFile(config.rootPath + "/dynamic/index.html")
	if err != nil {
		s.writeResponse(w, "", err)
		return
	}

	// substitute HTML template variables
	templateParsed, err := template.New("t").Parse(string(htmlTemplate))
	if err != nil {
		s.writeResponse(w, "", err)
		return
	}

	if err = templateParsed.Execute(w, htmlData); err != nil {
		s.writeResponse(w, "", err)
	}
}

// Process HTTP file upload request.
func (s *HTTPServer) handleUpload(w http.ResponseWriter, req *http.Request) {
	// limit request size to prevent DOS (10MB)
	req.Body = http.MaxBytesReader(w, req.Body, 10*1024*1024)

	// get file data from form
	if err := req.ParseMultipartForm(0); err != nil {
		s.writeResponse(w, "", err)
		return
	}

	newFile, handler, err := req.FormFile("file-input")
	if err != nil {
		s.writeResponse(w, "", err)
		return
	}

	// copy file from form to new local temp file
	tempFilePath := config.rootPath + "/db/temp/" + handler.Filename
	tempFile, err := os.OpenFile(tempFilePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		s.writeResponse(w, "", err)
		return
	}

	_, err = io.Copy(tempFile, newFile)
	if err != nil {
		os.Remove(tempFilePath)
		s.writeResponse(w, "", err)
		return
	}

	// close files before processing temp file
	newFile.Close()
	tempFile.Close()

	// process tags and people fields
	tags := ProcessInputList(req.Form.Get("tags-input"), ",")
	people := ProcessInputList(req.Form.Get("people-input"), ",")
	metaData := MetaData{Description: req.Form.Get("description-input"), Tags: tags, People: people}

	// add file to DB & move from db/temp dir to db/content dir
	fileAR := FileAccessRequest{errorOut: make(chan error), operation: "addFile", fileParam: tempFilePath, fileMetadata: metaData}
	s.fileDB.requestPool <- fileAR
	if err := <-fileAR.errorOut; err != nil {
		// destroy temp file on add failure
		os.Remove(tempFilePath)
		s.writeResponse(w, "", err)
		return
	}

	// upload to db/temp success
	http.Redirect(w, req, "/", 302)
}

// Write a HTTP response to connection.
func (s *HTTPServer) writeResponse(w http.ResponseWriter, response string, err error) {
	w.WriteHeader(http.StatusOK)
	if err != nil {
		log.Println(err)
		response = err.Error()
	}
	_, err = fmt.Fprintf(w, "%v\n", response)
	if err != nil {
		log.Println(err)
	}
}

// Gracefully stop the server and save DB to file.
func (s *HTTPServer) Stop() {
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	if err := s.server.Shutdown(ctx); err != nil {
		log.Println(err)
	}

	fileAR := FileAccessRequest{errorOut: make(chan error), operation: "serialize"}
	s.fileDB.requestPool <- fileAR
	if err := <-fileAR.errorOut; err != nil {
		log.Println(err)
	}
}
