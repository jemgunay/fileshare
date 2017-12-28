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
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"gopkg.in/gomail.v2"
)

// A server providing file sharing and access related services.
type ServerBase struct {
	fileDB         *FileDB
	startTimestamp int64
}

// Initialise servers.
func NewServerBase() (err error, httpServer HTTPServer) {
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
		log.Println(fmt.Errorf("host and port parameters must be specified in config"))
		return
	}
	httpPort, err := strconv.Atoi(config.params["http_port"].val)
	if err != nil {
		log.Println(fmt.Errorf("invalid port value found in config file"))
		return
	}
	httpServer = HTTPServer{host: config.params["http_host"].val, port: httpPort, ServerBase: ServerBase{fileDB: fileDB, startTimestamp: time.Now().Unix()}, userDB: userDB}
	httpServer.Start()

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
	router.HandleFunc("/login", s.authHandler(s.loginHandler)).Methods(http.MethodGet, http.MethodPost)
	router.HandleFunc("/logout", s.authHandler(s.logoutHandler)).Methods(http.MethodGet)
	router.HandleFunc("/reset", s.authHandler(s.resetHandler)).Methods(http.MethodGet)
	router.HandleFunc("/reset/{type}", s.authHandler(s.resetHandler)).Methods(http.MethodPost)
	router.HandleFunc("/users", s.authHandler(s.viewUsersHandler)).Methods(http.MethodGet)
	// memory data viewing
	router.HandleFunc("/", s.authHandler(s.viewMemoriesHandler)).Methods(http.MethodGet)
	router.HandleFunc("/search", s.authHandler(s.searchFilesHandler)).Methods(http.MethodGet)
	router.HandleFunc("/data", s.authHandler(s.getMetaDataHandler)).Methods(http.MethodGet)
	// upload
	router.HandleFunc("/upload", s.authHandler(s.uploadHandler)).Methods(http.MethodGet)
	router.HandleFunc("/upload/{type}", s.authHandler(s.uploadHandler)).Methods(http.MethodPost)
	// static uploaded file server
	staticFileHandler := http.StripPrefix("/static/", http.FileServer(http.Dir(config.rootPath+"/static/")))
	router.Handle(`/static/{rest:[a-zA-Z0-9=\-\/._]+}`, s.fileServerAuthHandler(staticFileHandler))
	// temp uploaded file server
	tempFileHandler := http.StripPrefix("/temp_uploaded/", http.FileServer(http.Dir(config.rootPath+"/db/temp/")))
	router.Handle(`/temp_uploaded/{user_id:[a-zA-Z0-9=\-_]+}/{file:[a-zA-Z0-9=\-\/._]+}`, s.fileServerAuthHandler(tempFileHandler))

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

		// file servers
		// prevent dir listings
		if r.URL.String() != "/" && strings.HasSuffix(r.URL.String(), "/") {
			s.respond(w, "404 page not found", false)
			return
		}
		// prevent unauthorised access to /static/content
		if strings.HasPrefix(r.URL.String(), "/static/") && !strings.HasPrefix(r.URL.String(), "/static/content/") {
			h(w, r)
			return
		}
		// prevent unauthorised access to temp uploaded files
		if strings.HasPrefix(r.URL.String(), "/temp_uploaded/") {
			vars := mux.Vars(r)

			userAR := UserAccessRequest{response: make(chan UserAccessResponse), operation: "getSessionUser", writerIn: w, reqIn: r}
			s.userDB.requestPool <- userAR
			id := (<-userAR.response).user.UUID

			if id != vars["user_id"] {
				s.respond(w, "404 page not found", false)
				return
			}
		}

		// if already logged in, redirect these page requests
		if r.URL.String() == "/login" || r.URL.String() == "/reset" {
			if response.success {
				if r.Method == http.MethodGet {
					http.Redirect(w, r, "/", 302)
				} else {
					s.respond(w, "already authenticated", false)
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

			if r.Method == http.MethodGet {
				http.Redirect(w, r, "/login", 302)
			} else {
				s.respond(w, "unauthorised", false)
			}
			return
		}

		// continue to call handler
		h(w, r)
	}
}

// File server auth wrapper.
func (s *HTTPServer) fileServerAuthHandler(h http.Handler) http.Handler {
	return s.authHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
	}))
}

// Handle user password reset request.
func (s *HTTPServer) resetHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	// fetch login form
	case http.MethodGet:
		// HTML template data
		templateData := struct {
			Title       string
			BrandName   string
			FooterHTML  template.HTML
			ContentHTML template.HTML
		}{
			"Reset Password",
			config.params["brand_name"].val,
			"",
			"",
		}

		var err error
		templateData.FooterHTML, err = s.completeTemplate("/dynamic/templates/footers/login_footer.html", templateData)
		templateData.ContentHTML, err = s.completeTemplate("/dynamic/templates/reset_password.html", templateData)
		result, err := s.completeTemplate("/dynamic/templates/main.html", templateData)
		if err != nil {
			s.respond(w, err.Error(), true)
		}

		s.respond(w, string(result), false)

	// submit login request
	case http.MethodPost:
		r.ParseForm()
		vars := mux.Vars(r)
		fmt.Println(vars)
		/*userAR := UserAccessRequest{response: make(chan UserAccessResponse), operation: "resetPassword", writerIn: w, reqIn: r}
		s.userDB.requestPool <- userAR
		response := <-userAR.response
		ok, err := response.success, response.err*/

		/*switch {
		case err != nil:
			s.respond(w, "error", false)
		case ok == false:
			s.respond(w, "unauthorised", false)
		case ok:
			s.respond(w, "success", false)
		}*/

		// email new randomly generated temp password
		msgBody := "this is your new temporary password: 'new password here'. It will expire in x number of hours."

		msg := gomail.NewMessage()
		msg.SetHeader("From", config.params["display_email_addr"].val)
		msg.SetHeader("To", "bob@example.com")
		msg.SetHeader("Subject", config.params["brand_name"].val + ": Password Reset")
		msg.SetBody("text/html", msgBody)

		port, err := strconv.Atoi(config.params["core_email_server"].val)
		if err != nil {
			s.respond(w, "error", true)
			return
		}

		d := gomail.NewDialer(config.params["core_email_server"].val, port, config.params["core_email_addr"].val, config.params["core_email_password"].val)

		// Send the email to Bob, Cora and Dan.
		if err := d.DialAndSend(msg); err != nil {
			s.respond(w, "error", true)
			return
		}
	}
}

// Handle login.
func (s *HTTPServer) loginHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	// fetch login form
	case http.MethodGet:
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
		templateData.FooterHTML, err = s.completeTemplate("/dynamic/templates/footers/login_footer.html", templateData)
		templateData.ContentHTML, err = s.completeTemplate("/dynamic/templates/login.html", templateData)
		result, err := s.completeTemplate("/dynamic/templates/main.html", templateData)
		if err != nil {
			s.respond(w, err.Error(), true)
		}

		s.respond(w, string(result), false)

	// submit login request
	case http.MethodPost:
		userAR := UserAccessRequest{response: make(chan UserAccessResponse), operation: "loginUser", writerIn: w, reqIn: r}
		s.userDB.requestPool <- userAR
		response := <-userAR.response
		ok, err := response.success, response.err

		switch {
		case err != nil:
			s.respond(w, "error", false)
		case ok == false:
			s.respond(w, "unauthorised", false)
		case ok:
			s.respond(w, "success", false)
		}
	}
}

// Handle logout.
func (s *HTTPServer) logoutHandler(w http.ResponseWriter, r *http.Request) {
	userAR := UserAccessRequest{response: make(chan UserAccessResponse), operation: "logoutUser", writerIn: w, reqIn: r}
	s.userDB.requestPool <- userAR

	if err := (<-userAR.response).err; err != nil {
		s.respond(w, "error", true)
		return
	}
	http.Redirect(w, r, "/login", 302)
}

// Process HTTP view files request.
func (s *HTTPServer) viewUsersHandler(w http.ResponseWriter, r *http.Request) {
	// get a list of all users from db
	userAR := UserAccessRequest{response: make(chan UserAccessResponse), operation: "getUsers"}
	s.userDB.requestPool <- userAR
	response := <-userAR.response

	// HTML template data
	templateData := struct {
		Title       string
		BrandName   string
		Users       map[string]User
		NavbarHTML  template.HTML
		NavbarFocus string
		FooterHTML  template.HTML
		FilesHTML   template.HTML
		ContentHTML template.HTML
	}{
		"Users",
		config.params["brand_name"].val,
		response.users,
		"",
		"users",
		"",
		"",
		"",
	}

	var err error
	templateData.NavbarHTML, err = s.completeTemplate("/dynamic/templates/navbar.html", templateData)
	templateData.FooterHTML, err = s.completeTemplate("/dynamic/templates/footers/search_footer.html", templateData)
	templateData.ContentHTML, err = s.completeTemplate("/dynamic/templates/users.html", templateData)
	result, err := s.completeTemplate("/dynamic/templates/main.html", templateData)
	if err != nil {
		s.respond(w, err.Error(), true)
		return
	}

	s.respond(w, string(result), false)
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
		filesListResult, err := s.completeTemplate("/dynamic/templates/files_list.html", templateData)
		if err != nil {
			s.respond(w, err.Error(), true)
			return
		}

		s.respond(w, string(filesListResult), false)
		return
	}

	// pretty print JSON?
	prettyPrint, err := strconv.ParseBool(q.Get("pretty"))
	if err != nil {
		prettyPrint = false
	}
	// JSON formatted response
	filesJSON := FilesToJSON(files, prettyPrint)
	s.respond(w, filesJSON, false)
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
		s.respond(w, err.Error(), true)
		return
	}
	s.respond(w, string(response), false)
}

// Process HTTP view files request.
func (s *HTTPServer) viewMemoriesHandler(w http.ResponseWriter, r *http.Request) {
	// get a list of all files from db
	searchReq := SearchRequest{fileTypes: ProcessInputList("image,video,audio,text,other", ",", true)}
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
	templateData.NavbarHTML, err = s.completeTemplate("/dynamic/templates/navbar.html", templateData)
	templateData.FooterHTML, err = s.completeTemplate("/dynamic/templates/footers/search_footer.html", templateData)
	var filesHTMLTarget = "/dynamic/templates/files_list.html"
	if len(templateData.Files) == 0 {
		filesHTMLTarget = "/static/templates/no_match.html"
	}
	templateData.FilesHTML, err = s.completeTemplate(filesHTMLTarget, templateData)
	templateData.ContentHTML, err = s.completeTemplate("/dynamic/templates/search.html", templateData)
	result, err := s.completeTemplate("/dynamic/templates/main.html", templateData)
	if err != nil {
		s.respond(w, err.Error(), true)
		return
	}

	s.respond(w, string(result), false)
}

// Process HTTP file upload request.
func (s *HTTPServer) uploadHandler(w http.ResponseWriter, r *http.Request) {
	// get user details
	userAR := UserAccessRequest{response: make(chan UserAccessResponse), operation: "getSessionUser", writerIn: w, reqIn: r}
	s.userDB.requestPool <- userAR
	userResponse := <-userAR.response
	if userResponse.err != nil {
		s.respond(w, userResponse.err.Error(), true)
		return
	}

	vars := mux.Vars(r)

	switch r.Method {
	case http.MethodGet:
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

			result, err := s.completeTemplate("/dynamic/templates/upload_form.html", uploadTemplateData)
			if err != nil {
				s.respond(w, err.Error(), true)
				return
			}

			templateData.UploadFormsHTML += result
		}

		templateData.NavbarHTML, err = s.completeTemplate("/dynamic/templates/navbar.html", templateData)
		templateData.FooterHTML, err = s.completeTemplate("/dynamic/templates/footers/upload_footer.html", templateData)
		templateData.ContentHTML, err = s.completeTemplate("/static/templates/upload.html", templateData)
		result, err := s.completeTemplate("/dynamic/templates/main.html", templateData)
		if err != nil {
			s.respond(w, err.Error(), true)
		}

		s.respond(w, string(result), false)

	// file upload
	case http.MethodPost:
		// store file in temp dir under user UUID subdir ready for processing
		switch vars["type"] {
		case "temp":
			// limit request size to prevent DOS (10MB) & get data from form
			r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)
			if err := r.ParseMultipartForm(0); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				s.respond(w, err.Error(), false)
				return
			}
			// move form file to temp dir
			tempPath, tempName, err := s.fileDB.uploadFileToTemp(w, r, userResponse.user)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				s.respond(w, err.Error(), false)
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

			result, err := s.completeTemplate("/dynamic/templates/upload_form.html", templateData)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				s.respond(w, err.Error(), true)
				return
			}
			s.respond(w, string(result), false)

		// delete a file from user temp dir
		case "temp_delete":
			if err := r.ParseForm(); err != nil {
				s.respond(w, err.Error(), true)
				return
			}

			// construct temp file path
			targetTempDir := config.rootPath + "/db/temp/" + userResponse.user.UUID + "/"
			targetFile := r.Form.Get("file")

			// check if file exists
			ok, err := FileOrDirExists(targetTempDir + targetFile)
			if err != nil || !ok {
				s.respond(w, "invalid_file", false)
				return
			}

			// remove file
			if err := os.Remove(targetTempDir + targetFile); err != nil {
				s.respond(w, err.Error(), true)
				return
			}

			s.respond(w, "success", false)

		// move temp file to DB with file details
		case "store":
			if err := r.ParseForm(); err != nil {
				s.respond(w, err.Error(), true)
				return
			}

			// ensure access to target file is permitted
			targetTempDir := config.rootPath + "/db/temp/" + userResponse.user.UUID + "/"
			targetFile := r.Form.Get("file")
			ok, err := FileOrDirExists(targetTempDir + targetFile)
			if !ok || err != nil {
				s.respond(w, "temp file does not exist", false)
				return
			}

			// process tags and people fields
			tags := ProcessInputList(r.Form.Get("tags-input"), ",", true)
			people := ProcessInputList(r.Form.Get("people-input"), ",", true)
			metaData := MetaData{Description: r.Form.Get("description-input"), Tags: tags, People: people}

			// validate form field lengths
			if len(tags) == 0 {
				s.respond(w, "no_tags", false)
				return
			}
			if len(people) == 0 {
				s.respond(w, "no_people", false)
				return
			}

			// add file to DB & move from db/temp dir to db/content dir
			fileAR := FileAccessRequest{errorOut: make(chan error), operation: "storeFile", fileParam: targetTempDir + targetFile, fileMetadata: metaData}
			s.fileDB.requestPool <- fileAR
			if err := <-fileAR.errorOut; err != nil {
				s.respond(w, err.Error(), true)
				return
			}

			// success
			s.respond(w, "success", false)
		}
	}
}

// Write a HTTP response to connection.
func (s *HTTPServer) respond(w http.ResponseWriter, response string, enableLog bool) {
	if enableLog {
		log.Println(response)
	}
	// write
	if _, err := fmt.Fprintf(w, "%v\n", response); err != nil {
		log.Println(err)
	}
}

// Replace variables in HTML templates with corresponding values in TemplateData.
func (s *HTTPServer) completeTemplate(filePath string, data interface{}) (result template.HTML, err error) {
	filePath = config.rootPath + filePath

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
