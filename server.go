package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"os"
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

	// create new user DB
	userDB, err := NewUserDB(config.rootPath + "/db")
	if err != nil {
		log.Printf("Server error: %v", err.Error())
		return
	}

	// start file manager http server
	if config.params["http_host"].val == "" || config.params["http_port"].val == "" {
		err = fmt.Errorf("host and port parameters must be specified in config")
		log.Println(err)
		return
	}
	httpPort, err := strconv.Atoi(config.params["http_port"].val)
	if err != nil {
		err = fmt.Errorf("invalid port value found in config file")
		log.Println(err)
		return
	}
	httpServer = HTTPServer{host: config.params["http_host"].val, port: httpPort, ServerBase: ServerBase{fileDB: fileDB, startTimestamp: time.Now().Unix()}, userDB: userDB}
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
	userDB *UserDB
}

// Start listening for HTTP requests.
func (s *HTTPServer) Start() {
	// define HTTP routes
	router := mux.NewRouter()

	// user
	router.HandleFunc("/login", s.authHandler(s.loginHandler)).Methods("GET", "POST")
	router.HandleFunc("/logout", s.authHandler(s.logoutHandler)).Methods("GET")
	router.HandleFunc("/request", s.authHandler(s.requestAccessHandler)).Methods("GET", "POST")
	// memory data viewing
	router.HandleFunc("/", s.authHandler(s.viewMemoriesHandler)).Methods("GET")
	router.HandleFunc("/search", s.authHandler(s.searchFilesHandler)).Methods("GET")
	router.HandleFunc("/data", s.authHandler(s.getMetaDataHandler)).Methods("GET")
	// upload
	router.HandleFunc("/upload", s.authHandler(s.uploadHandler)).Methods("GET")
	router.HandleFunc("/upload/{type}", s.authHandler(s.uploadHandler)).Methods("POST")
	// static file servers
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir(config.rootPath+"/static/"))))
	router.PathPrefix("/temp_uploaded/").Handler(http.StripPrefix("/temp_uploaded/", http.FileServer(http.Dir(config.rootPath+"/db/temp/"))))

	s.server = &http.Server{
		Handler:      router,
		Addr:         s.host + ":" + fmt.Sprintf("%d", s.port),
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

// Request handler wrapper for auth.
func (s *HTTPServer) authHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// authenticate
		userAR := UserAccessRequest{response: make(chan UserAccessResponse), operation: "authenticateUser", writerIn: w, reqIn: r}
		s.userDB.requestPool <- userAR
		response := <-userAR.response

		// if already logged in, redirect these page requests
		if r.URL.String() == "/login" || r.URL.String() == "/request" {
			if response.success {
				if r.Method == "GET" {
					http.Redirect(w, r, "/", 302)
				} else {
					s.writeResponse(w, "", nil)
				}
			} else {
				h(w, r)
				return
			}
		}

		// if auth failed (error or wrong password)
		if response.err != nil || response.success == false {
			if response.err != nil {
				log.Println(response.err)
			}

			if r.Method == "GET" {
				http.Redirect(w, r, "/login", 302)
			} else {
				s.writeResponse(w, "unauthorised", response.err)
			}
			return
		}

		// continue to call handler
		h(w, r)
	}
}

// Search request query container.
type SearchRequest struct {
	description string
	tags        []string
	people      []string
	minDate     int64
	maxDate     int64
	fileTypes   []string
}

// Handle user registration.
func (s *HTTPServer) requestAccessHandler(w http.ResponseWriter, r *http.Request) {
	s.writeResponse(w, "register page", nil)
}

// Handle login.
func (s *HTTPServer) loginHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	// fetch login form
	case "GET":
		// HTML template data
		templateData := struct {
			Title       string
			BrandName   string
			FooterHTML  template.HTML
			ContentHTML template.HTML
		}{
			"Login",
			config.params["brand_name"].val,
			"",
			"",
		}

		var err error
		templateData.FooterHTML, err = s.completeTemplate(config.rootPath+"/static/login_footer.html", templateData)
		templateData.ContentHTML, err = s.completeTemplate(config.rootPath+"/dynamic/login.html", templateData)
		result, err := s.completeTemplate(config.rootPath+"/dynamic/main.html", templateData)

		s.writeResponse(w, string(result), err)

	// submit login request
	case "POST":
		userAR := UserAccessRequest{response: make(chan UserAccessResponse), operation: "loginUser", writerIn: w, reqIn: r}
		s.userDB.requestPool <- userAR
		response := <-userAR.response
		ok, err := response.success, response.err

		switch {
		case err != nil:
			log.Println(err)
			s.writeResponse(w, "error", nil)
		case ok == false:
			s.writeResponse(w, "unauthorised", nil)
		case ok:
			s.writeResponse(w, "success", nil)
		}
	}
}

// Handle logout.
func (s *HTTPServer) logoutHandler(w http.ResponseWriter, r *http.Request) {
	userAR := UserAccessRequest{response: make(chan UserAccessResponse), operation: "logoutUser", writerIn: w, reqIn: r}
	s.userDB.requestPool <- userAR
	err := (<-userAR.response).err

	if err != nil {
		log.Println(err)
		s.writeResponse(w, "error", err)
	}
	http.Redirect(w, r, "/login", 302)
}

// Search files by their properties.
// URL params: [desc, start_date, end_date, file_types, tags, people, format(json/html), pretty]
func (s *HTTPServer) searchFilesHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	// construct search query from url params
	searchReq := SearchRequest{description: q.Get("desc"), minDate: 0, maxDate: 0}
	searchReq.tags = ProcessInputList(q.Get("tags"), ",", true)
	searchReq.people = ProcessInputList(q.Get("people"), ",", true)
	searchReq.fileTypes = ProcessInputList(q.Get("file_types"), ",", true)
	// parse date to int unix timestamp
	if formattedDate, err := strconv.ParseInt(q.Get("min_date"), 10, 64); err == nil {
		searchReq.minDate = formattedDate
	}
	if formattedDate, err := strconv.ParseInt(q.Get("max_date"), 10, 64); err == nil {
		searchReq.maxDate = formattedDate
	}

	// perform search
	fileAR := FileAccessRequest{filesOut: make(chan []File), operation: "search", searchParams: searchReq}
	s.fileDB.requestPool <- fileAR
	files := <-fileAR.filesOut

	// respond with JSON or HTML?
	if q.Get("format") == "html" {
		// HTML formatted response
		templateData := struct {
			Files []File
		}{
			files,
		}
		filesListResult, err := s.completeTemplate(config.rootPath+"/dynamic/files_list.html", templateData)

		s.writeResponse(w, string(filesListResult), err)
		return
	}

	// pretty print JSON?
	prettyPrint, err := strconv.ParseBool(q.Get("pretty"))
	if err != nil {
		prettyPrint = false
	}
	// JSON formatted response
	filesJSON := FilesToJSON(files, prettyPrint)
	s.writeResponse(w, filesJSON, nil)
}

// Get specific JSON data such as all tags & people.
// URL params (data is returned for metadata types included in the fetch param): ?fetch=tags,people,file_types,dates
func (s *HTTPServer) getMetaDataHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	resultsList := make(map[string][]string)

	metaDataTypes := ProcessInputList(q.Get("fetch"), ",", true)
	for _, meta := range metaDataTypes {
		// perform data request
		fileAR := FileAccessRequest{stringsOut: make(chan []string), operation: "getMetaData", target: meta}
		s.fileDB.requestPool <- fileAR
		data := <-fileAR.stringsOut

		resultsList[meta] = data
	}

	// parse query result to json
	response, err := json.Marshal(resultsList)
	if err != nil {
		response = []byte(err.Error())
	}
	s.writeResponse(w, string(response), nil)
}

// Process HTTP view files request.
func (s *HTTPServer) viewMemoriesHandler(w http.ResponseWriter, r *http.Request) {
	// get a list of all files from db
	searchReq := SearchRequest{fileTypes: ProcessInputList("image,video,audio", ",", true)}
	fileAR := FileAccessRequest{filesOut: make(chan []File), operation: "search", searchParams: searchReq}
	s.fileDB.requestPool <- fileAR
	files := <-fileAR.filesOut

	// HTML template data
	templateData := struct {
		Title       string
		BrandName   string
		Files       []File
		NavbarHTML  template.HTML
		NavbarFocus string
		FooterHTML  template.HTML
		FilesHTML   template.HTML
		ContentHTML template.HTML
	}{
		"Memories",
		config.params["brand_name"].val,
		files,
		"",
		"search",
		"",
		"",
		"",
	}

	var err error
	templateData.NavbarHTML, err = s.completeTemplate(config.rootPath+"/dynamic/navbar.html", templateData)
	templateData.FooterHTML, err = s.completeTemplate(config.rootPath+"/static/search_footer.html", templateData)
	var filesHTMLTarget = "/dynamic/files_list.html"
	if len(templateData.Files) == 0 {
		filesHTMLTarget = "/static/no_match.html"
	}
	templateData.FilesHTML, err = s.completeTemplate(config.rootPath+filesHTMLTarget, templateData)
	templateData.ContentHTML, err = s.completeTemplate(config.rootPath+"/dynamic/home.html", templateData)
	result, err := s.completeTemplate(config.rootPath+"/dynamic/main.html", templateData)

	s.writeResponse(w, string(result), err)
}

// Process HTTP file upload request.
func (s *HTTPServer) uploadHandler(w http.ResponseWriter, r *http.Request) {
	// get user details
	userAR := UserAccessRequest{response: make(chan UserAccessResponse), operation: "getSessionUser", writerIn: w, reqIn: r}
	s.userDB.requestPool <- userAR
	userResponse := <-userAR.response
	if userResponse.err != nil {
		s.writeResponse(w, "", userResponse.err)
		return
	}

	vars := mux.Vars(r)

	switch r.Method {
	case "GET":
		// fetch upload page
		templateData := struct {
			Title           string
			BrandName       string
			NavbarHTML      template.HTML
			NavbarFocus     string
			FooterHTML      template.HTML
			UploadHTML      template.HTML
			ContentHTML     template.HTML
			UploadFormsHTML template.HTML
		}{
			"Upload",
			config.params["brand_name"].val,
			"",
			"upload",
			"",
			"",
			"",
			"",
		}

		// get all files in temp dir for current session user
		files, err := ioutil.ReadDir(config.rootPath + "/db/temp/" + userResponse.user.UUID + "/")

		// generate upload description forms for each unpublished image
		for _, f := range files {
			uploadTemplateData := struct {
				FileName string
				FilePath string
			}{
				f.Name(),
				"/temp_uploaded/" + userResponse.user.UUID + "/",
			}

			result, err := s.completeTemplate(config.rootPath+"/dynamic/upload_form.html", uploadTemplateData)
			if err != nil {
				s.writeResponse(w, "", err)
				return
			}

			templateData.UploadFormsHTML += result
		}

		//var err error
		templateData.NavbarHTML, err = s.completeTemplate(config.rootPath+"/dynamic/navbar.html", templateData)
		templateData.FooterHTML, err = s.completeTemplate(config.rootPath+"/static/upload_footer.html", templateData)
		templateData.ContentHTML, err = s.completeTemplate(config.rootPath+"/static/upload.html", templateData)
		result, err := s.completeTemplate(config.rootPath+"/dynamic/main.html", templateData)

		s.writeResponse(w, string(result), err)

	// file upload
	case "POST":
		// store file in temp dir under user UUID subdir ready for processing
		switch vars["type"] {
		case "temp":
			// limit request size to prevent DOS (10MB) & get data from form
			r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)
			if err := r.ParseMultipartForm(0); err != nil {
				s.writeResponse(w, "", err)
			}
			// move form file to temp dir
			tempPath, tempName, err := s.fileDB.uploadFileToTemp(w, r, userResponse.user)
			if err != nil {
				s.writeResponse(w, "", err)
				return
			}

			// html details form response
			templateData := struct {
				FileName string
				FilePath string
			}{
				tempName,
				tempPath,
			}

			result, err := s.completeTemplate(config.rootPath+"/dynamic/upload_form.html", templateData)
			s.writeResponse(w, string(result), err)

		// delete a file from user temp dir
		case "temp_delete":
			if err := r.ParseForm(); err != nil {
				s.writeResponse(w, "", err)
				return
			}

			// construct temp file path
			targetTempDir := config.rootPath + "/db/temp/" + userResponse.user.UUID + "/"
			targetFile := r.Form.Get("file")

			// check if file exists
			ok, err := FileOrDirExists(targetTempDir + targetFile)
			if err != nil || !ok {
				s.writeResponse(w, "", fmt.Errorf("invalid_file"))
				return
			}

			// remove file
			if err := os.Remove(targetTempDir + targetFile); err != nil {
				s.writeResponse(w, "", err)
				return
			}

			s.writeResponse(w, "success", nil)

		// move temp file to DB with file details
		case "store":
			if err := r.ParseForm(); err != nil {
				s.writeResponse(w, "", err)
				return
			}

			// ensure access to target file is permitted
			targetTempDir := config.rootPath + "/db/temp/" + userResponse.user.UUID + "/"
			targetFile := r.Form.Get("file")
			ok, err := FileOrDirExists(targetTempDir + targetFile)
			if !ok || err != nil {
				s.writeResponse(w, "", fmt.Errorf("temp file does not exist"))
				return
			}

			// process tags and people fields
			tags := ProcessInputList(r.Form.Get("tags-input"), ",", true)
			people := ProcessInputList(r.Form.Get("people-input"), ",", true)
			metaData := MetaData{Description: r.Form.Get("description-input"), Tags: tags, People: people}

			// validate form field lengths
			if len(tags) == 0 {
				s.writeResponse(w, "", fmt.Errorf("no_tags"))
			}
			if len(people) == 0 {
				s.writeResponse(w, "", fmt.Errorf("no_people"))
			}

			// add file to DB & move from db/temp dir to db/content dir
			fileAR := FileAccessRequest{errorOut: make(chan error), operation: "storeFile", fileParam: targetTempDir + targetFile, fileMetadata: metaData}
			s.fileDB.requestPool <- fileAR
			if err := <-fileAR.errorOut; err != nil {
				s.writeResponse(w, "", err)
				return
			}

			// success
			s.writeResponse(w, "success", err)
		}
	}
}

// Write a HTTP response to connection.
func (s *HTTPServer) writeSuccess(w http.ResponseWriter, response string) {
	// write
	_, err := fmt.Fprintf(w, "%v\n", response)
	if err != nil {
		log.Println(err)
	}
}

// Write a HTTP error response to connection.
func (s *HTTPServer) writeError(w http.ResponseWriter, err error, enableLog bool) {
	if enableLog {
		log.Println(err)
		w.WriteHeader(http.StatusForbidden)
	}
	// write
	_, err = fmt.Fprintf(w, "%v\n", err.Error())
	if err != nil {
		log.Println(err)
	}
}

// Replace variables in HTML templates with corresponding values in TemplateData.
func (s *HTTPServer) completeTemplate(filePath string, data interface{}) (result template.HTML, err error) {
	// load HTML template from disk
	htmlTemplate, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Println(err)
		return
	}

	// parse HTML template
	templateParsed, err := template.New("t").Parse(string(htmlTemplate))
	if err != nil {
		log.Println(err)
		return
	}

	// perform template variable replacement
	buffer := new(bytes.Buffer)
	if err = templateParsed.Execute(buffer, data); err != nil {
		log.Println(err)
		return
	}

	return template.HTML(buffer.String()), nil
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
