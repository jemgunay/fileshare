package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"os"

	"github.com/gorilla/mux"
)

// A server providing file sharing and access related services.
type ServerBase struct {
	startTimestamp int64
	fileDB         *FileDB
}

// Initialise a new file server.
func NewServerBase() (serverBase *ServerBase, err error) {
	fileDB, err := NewFileDB(rootPath + "/db")
	if err != nil {
		log.Printf("Server error: %v", err.Error())
		return
	}

	// load server config
	serverBase = &ServerBase{fileDB: fileDB, startTimestamp: time.Now().Unix()}
	config.LoadConfig(rootPath)

	//fileDB.AddFile(rootPath + "/test/test_1.png")

	httpServer := &HTTPServer{host: "localhost", port: 8000, serverBase: serverBase}
	go httpServer.Start()

	//fmt.Printf("%#v \n", fileDB.data)
	//fmt.Println(config.params["version"])

	// start hosting HTTP server to access local file DB (via web UI, with authentication)

	// get a list of other currently online servers providing file updates (via C&C web server) + from local config file containing previously known servers/manually added servers

	// provide these servers with a log of currently owned file hashes, requesting for files we do not own (everyone must have a complete log of all operations)

	// retrieve all files we do not own from the server (one request at a time) + retrieve remote files marked for deletion also, and delete those locally

	// once file DB is up to date, start hosting files to remote servers

	return
}

type HTTPServer struct {
	host       string
	port       int
	serverBase *ServerBase
}

// Start listening for HTTP requests.
func (s *HTTPServer) Start() {
	// define HTTP routes
	router := mux.NewRouter()
	// pull new content from channel
	router.HandleFunc("/upload/", s.handleUpload).Methods("POST")
	// serve static files
	//router.PathPrefix("/").Handler(http.StripPrefix("/", http.FileServer(http.Dir(rootPath+"/static/")))).Methods("GET")
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir(rootPath+"/static/"))))
	router.PathPrefix("/content/").Handler(http.StripPrefix("/content/", http.FileServer(http.Dir(rootPath+"/db/content/"))))

	// generate random port for http server and open in browser
	/*s1 := rand.NewSource(time.Now().UnixNano())
	r1 := rand.New(s1)
	s.port = s.port + 1 + r1.Intn(1000)*/
	//go openBrowser("http://" + client.host + ":" + strconv.Itoa(s.port))

	// listen for HTTP requests
	log.Printf("starting HTTP server on port %d", s.port)
	err := http.ListenAndServe(s.host+":"+strconv.Itoa(s.port), router)
	if err != nil {
		log.Println(err)
	}
}

// Process HTTP client file upload request.
func (s *HTTPServer) handleUpload(w http.ResponseWriter, req *http.Request) {
	// get file data from form
	newFile, header, err := req.FormFile("file")
	if err != nil {
		s.writeResponse(w, "", err)
		return
	}

	// copy file from form to new local temp file
	tempFilePath := rootPath + "/db/temp/" + header.Filename
	tempFile, err := os.OpenFile(tempFilePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		s.writeResponse(w, "", err)
		return
	}

	_, err = io.Copy(tempFile, newFile)
	if err != nil {
		s.writeResponse(w, "", err)
		return
	}

	// close files before processing temp file
	newFile.Close()
	tempFile.Close()

	// add file to DB & move from db/temp dir to db/content dir
	fileAccessReq := FileAccessRequest{errorOut: make(chan error, 1), operation: "addFile", fileParam: tempFilePath}
	s.serverBase.fileDB.requestPool <- fileAccessReq
	if err := <-fileAccessReq.errorOut; err != nil {
		s.writeResponse(w, "", err)
		return
	}

	// upload to db/temp success
	s.writeResponse(w, "<html><img src='http://"+s.host+":"+strconv.Itoa(s.port)+"/content/"+header.Filename+"'><html>", nil)
}

// Write a HTTP response.
func (s *HTTPServer) writeResponse(w http.ResponseWriter, response string, err error) {
	if err != nil {
		log.Println(err)
		response = err.Error()
	}
	_, err = fmt.Fprintf(w, "%v", response)
	if err != nil {
		log.Println(err)
	}

}
