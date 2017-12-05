package main

import (
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"os"

	"io/ioutil"

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

	// view all files
	router.HandleFunc("/", s.viewFiles).Methods("GET")
	// upload file
	router.HandleFunc("/upload/", s.handleUpload).Methods("POST")
	// serve static files
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir(rootPath+"/static/"))))
	//router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir(rootPath+"/static/"))))
	//router.PathPrefix("/content/").Handler(http.StripPrefix("/content/", http.FileServer(http.Dir(rootPath+"/db/content/"))))

	server := &http.Server{
		Handler:      router,
		Addr:         s.host + ":" + strconv.Itoa(s.port),
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	// generate random port for http server and open in browser
	/*s1 := rand.NewSource(time.Now().UnixNano())
	r1 := rand.New(s1)
	s.port = s.port + 1 + r1.Intn(1000)*/

	// listen for HTTP requests
	log.Printf("starting HTTP server on port %d", s.port)
	// add HTTPS: https://www.kaihag.com/https-and-go/
	err := server.ListenAndServe()
	if err != nil {
		log.Println(err)
	}
}

// Process HTTP view files request.
func (s *HTTPServer) viewFiles(w http.ResponseWriter, req *http.Request) {
	fileAccessReq := FileAccessRequest{filesOut: make(chan []File), operation: "toString"}
	s.serverBase.fileDB.requestPool <- fileAccessReq
	files := <-fileAccessReq.filesOut

	// html template data
	htmlData := struct {
		Title string
		Files []File
	}{
		"Home",
		files,
	}

	// load HTML template from disk
	htmlTemplate, err := ioutil.ReadFile(rootPath + "/dynamic/index.html")
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
	// limit request size to prevent DOS (5MB)
	//req.Body = http.MaxBytesReader(w, req.Body, 5*1024*1024)

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
	tempFilePath := rootPath + "/db/temp/" + handler.Filename
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
	fileAccessReq := FileAccessRequest{errorOut: make(chan error), operation: "addFile", fileParam: tempFilePath}
	s.serverBase.fileDB.requestPool <- fileAccessReq
	if err := <-fileAccessReq.errorOut; err != nil {
		s.writeResponse(w, "", err)
		return
	}

	// upload to db/temp success
	http.Redirect(w, req, "/", 302)
}

// Write a HTTP response.
func (s *HTTPServer) writeResponse(w http.ResponseWriter, response string, err error) {
	if err != nil {
		log.Println(err)
		response = err.Error()
	}
	_, err = fmt.Fprintf(w, "%v\n", response)
	if err != nil {
		log.Println(err)
	}
}
