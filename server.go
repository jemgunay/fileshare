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
	router.HandleFunc("/reset", s.resetHandler).Methods(http.MethodGet)
	router.HandleFunc("/reset/{type}", s.resetHandler).Methods(http.MethodPost)
	router.HandleFunc("/users", s.authHandler(s.viewUsersHandler)).Methods(http.MethodGet)
	// memory data viewing
	router.HandleFunc("/", s.authHandler(s.viewMemoriesHandler)).Methods(http.MethodGet)
	router.HandleFunc("/view", s.authHandler(s.viewMemoriesHandler)).Methods(http.MethodPost)
	router.HandleFunc("/search", s.authHandler(s.searchMemoriesHandler)).Methods(http.MethodGet)
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
		authResponse := s.userDB.performAccessRequest(UserAccessRequest{operation: "authenticateUser", w: w, r: r})

		// file servers
		// prevent dir listings
		if r.URL.String() != "/" && strings.HasSuffix(r.URL.String(), "/") {
			s.respond(w, "404 page not found", 3)
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

			userResponse := s.userDB.performAccessRequest(UserAccessRequest{operation: "getSessionUser", w: w, r: r})
			if userResponse.user.UUID != vars["user_id"] {
				s.respond(w, "404 page not found", 3)
				return
			}
		}

		// if already logged in, redirect these page requests
		if r.URL.String() == "/login" {
			if authResponse.success {
				if r.Method == http.MethodGet {
					http.Redirect(w, r, "/", 302)
				} else {
					s.respond(w, "already authenticated", 3)
				}
			} else {
				h(w, r)
				return
			}
		}
		// if auth failed (error or wrong password)
		if authResponse.err != nil || authResponse.success == false {
			if authResponse.err != nil {
				log.Println(authResponse.err)
			}

			if r.Method == http.MethodGet {
				http.Redirect(w, r, "/login", 302)
			} else {
				s.respond(w, "unauthorised", 3)
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

		templateData.FooterHTML = s.completeTemplate("/dynamic/templates/footers/login_footer.html", templateData)
		templateData.ContentHTML = s.completeTemplate("/dynamic/templates/reset_password.html", templateData)
		result := s.completeTemplate("/dynamic/templates/main.html", templateData)

		s.respond(w, string(result), 3)

	// submit password reset request
	case http.MethodPost:
		r.ParseForm()
		vars := mux.Vars(r)
		fmt.Println(vars)

		/*response := s.userDB.performAccessRequest(UserAccessRequest{operation: "resetPassword", writerIn: w, reqIn: r})
		ok, err := response.success, response.err*/

		/*switch {
		case err != nil:
			s.respond(w, "error", 2)
		case ok == false:
			s.respond(w, "unauthorised", 3)
		case ok:
			s.respond(w, "success", 3)
		}*/

		// email new randomly generated temp password
		msgBody := "This is your new temporary password: 'new password here'. Use it to log in and change your password. It will expire in 30 minutes."

		msg := gomail.NewMessage()
		msg.SetHeader("From", "admin@memoryshare.com") // config.params["display_email_addr"].val
		msg.SetHeader("To", "jemgunay@yahoo.co.uk")
		msg.SetHeader("Subject", config.params["brand_name"].val+": Password Reset")
		msg.SetBody("text/html", msgBody)

		/*port, err := strconv.Atoi(config.params["core_email_port"].val)
		if err != nil {
			s.respond(w, "error", 2)
			return
		}*/

		//d := gomail.NewDialer(config.params["core_email_server"].val, port, config.params["core_email_addr"].val, config.params["core_email_password"].val)

		d := gomail.NewDialer("smtp.gmail.com", 465, config.params["core_email_addr"].val, config.params["core_email_password"].val)

		// Send the email to Bob, Cora and Dan.
		if err := d.DialAndSend(msg); err != nil {
			log.Println(err)
			s.respond(w, "error", 2)
			return
		}

		s.respond(w, "success", 3)
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

		templateData.FooterHTML = s.completeTemplate("/dynamic/templates/footers/login_footer.html", templateData)
		templateData.ContentHTML = s.completeTemplate("/dynamic/templates/login.html", templateData)
		result := s.completeTemplate("/dynamic/templates/main.html", templateData)

		s.respond(w, string(result), 3)

	// submit login request
	case http.MethodPost:
		response := s.userDB.performAccessRequest(UserAccessRequest{operation: "loginUser", w: w, r: r})

		switch {
		case response.err != nil:
			s.respond(w, "error", 2)
		case response.success == false:
			s.respond(w, "unauthorised", 3)
		case response.success:
			s.respond(w, "success", 3)
		}
	}
}

// Handle logout.
func (s *HTTPServer) logoutHandler(w http.ResponseWriter, r *http.Request) {
	response := s.userDB.performAccessRequest(UserAccessRequest{operation: "logoutUser", w: w, r: r})

	if response.err != nil {
		s.respond(w, "error", 2)
		return
	}
	http.Redirect(w, r, "/login", 302)
}

// Process HTTP view files request.
func (s *HTTPServer) viewUsersHandler(w http.ResponseWriter, r *http.Request) {
	response := s.userDB.performAccessRequest(UserAccessRequest{operation: "getUsers"})

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

	templateData.NavbarHTML = s.completeTemplate("/dynamic/templates/navbar.html", templateData)
	templateData.FooterHTML = s.completeTemplate("/dynamic/templates/footers/search_footer.html", templateData)
	templateData.ContentHTML = s.completeTemplate("/dynamic/templates/users.html", templateData)
	result := s.completeTemplate("/dynamic/templates/main.html", templateData)

	s.respond(w, string(result), 3)
}

// Search request query container.
type SearchRequest struct {
	description    string
	tags           []string
	people         []string
	minDate        int64
	maxDate        int64
	fileTypes      []string
	resultsPerPage int64
	page           int64
}

// Search files by their properties.
// URL params: [desc, start_date, end_date, file_types, tags, people, format(json/html_tiled/html_detailed), pretty]
func (s *HTTPServer) searchMemoriesHandler(w http.ResponseWriter, r *http.Request) {
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
	// parse pagination fields
	if formattedResultsCount, err := strconv.ParseInt(q.Get("results_per_page"), 10, 64); err == nil {
		searchReq.resultsPerPage = formattedResultsCount
	}
	if formattedResultsPage, err := strconv.ParseInt(q.Get("page"), 10, 64); err == nil {
		searchReq.page = formattedResultsPage
	}

	// perform search
	response := s.fileDB.performAccessRequest(FileAccessRequest{operation: "search", searchParams: searchReq})

	// respond with JSON or HTML?
	if q.Get("format") == "html_tiled" || q.Get("format") == "html_detailed" {
		// HTML formatted response
		templateData := struct {
			Files []File
		}{
			response.files,
		}
		// determine which template format to use
		templateFile := "/dynamic/templates/files_list_detailed.html"
		if q.Get("format") == "html_tiled" {
			templateFile = "/dynamic/templates/files_list_tiled.html"
		}

		if len(response.files) == 0 {
			templateFile = "/static/templates/no_match.html"
		}

		filesListResult := s.completeTemplate(templateFile, templateData)
		s.respond(w, string(filesListResult), 3)
		return
	}

	// pretty print JSON?
	prettyPrint, err := strconv.ParseBool(q.Get("pretty"))
	if err != nil {
		prettyPrint = false
	}
	// JSON formatted response
	filesJSON := FilesToJSON(response.files, prettyPrint)
	s.respond(w, filesJSON, 3)
}

// Get specific JSON data such as all tags & people.
// URL params (data is returned for metadata types included in the fetch param): ?fetch=tags,people,file_types,dates
func (s *HTTPServer) getMetaDataHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	resultsList := make(map[string][]string)

	metaDataTypes := ProcessInputList(q.Get("fetch"), ",", true)
	for _, dataType := range metaDataTypes {
		response := s.fileDB.performAccessRequest(FileAccessRequest{operation: "getMetaData", target: dataType})
		resultsList[dataType] = response.metaData
	}

	// parse query result to json
	response, err := json.Marshal(resultsList)
	if err != nil {
		s.respond(w, err.Error(), 1)
		return
	}
	s.respond(w, string(response), 3)
}

// Process HTTP view files request.
func (s *HTTPServer) viewMemoriesHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// get a list of all files from db
		searchReq := SearchRequest{fileTypes: ProcessInputList("image,video,audio,text,other", ",", true), resultsPerPage: 10}
		response := s.fileDB.performAccessRequest(FileAccessRequest{operation: "search", searchParams: searchReq})

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
			response.files,
			"",
			"search",
			"",
			"",
			"",
		}

		templateData.NavbarHTML = s.completeTemplate("/dynamic/templates/navbar.html", templateData)
		templateData.FooterHTML = s.completeTemplate("/dynamic/templates/footers/search_footer.html", templateData)
		var filesHTMLTarget = "/dynamic/templates/files_list_detailed.html"
		if len(templateData.Files) == 0 {
			filesHTMLTarget = "/static/templates/no_match.html"
		}
		templateData.FilesHTML = s.completeTemplate(filesHTMLTarget, templateData)
		templateData.ContentHTML = s.completeTemplate("/dynamic/templates/search.html", templateData)
		result := s.completeTemplate("/dynamic/templates/main.html", templateData)

		s.respond(w, string(result), 3)

	case http.MethodPost:
		vars := mux.Vars(r)
		if vars["UUID"] == "" {
			s.respond(w, "no file UUID provided", 3)
			return
		}

		// get a list of all files from db
		response := s.fileDB.performAccessRequest(FileAccessRequest{operation: "fetchMemory", target: vars["UUID"]})

		s.respond(w, respo, 3)
	}
}

// Process HTTP file upload request.
func (s *HTTPServer) uploadHandler(w http.ResponseWriter, r *http.Request) {
	// get user details
	userResponse := s.userDB.performAccessRequest(UserAccessRequest{operation: "getSessionUser", w: w, r: r})
	if userResponse.err != nil {
		s.respond(w, userResponse.err.Error(), 2)
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

		// get all temp files for
		files := s.fileDB.performAccessRequest(FileAccessRequest{operation: "getFilesByUser", UUID: userResponse.user.UUID, state: UPLOADED})

		// generate upload description forms for each unpublished image
		for _, f := range files.files {
			uploadTemplateData := struct {
				UploadedFile File
			}{
				f,
			}

			result := s.completeTemplate("/dynamic/templates/upload_form.html", uploadTemplateData)
			templateData.UploadFormsHTML += result
		}

		templateData.NavbarHTML = s.completeTemplate("/dynamic/templates/navbar.html", templateData)
		templateData.FooterHTML = s.completeTemplate("/dynamic/templates/footers/upload_footer.html", templateData)
		templateData.ContentHTML = s.completeTemplate("/dynamic/templates/upload.html", templateData)
		result := s.completeTemplate("/dynamic/templates/main.html", templateData)

		s.respond(w, string(result), 3)

	// file upload
	case http.MethodPost:
		// upload file to temp dir under user UUID subdir ready for processing (only uploading user can access)
		switch vars["type"] {
		case "temp":
			// limit request size to prevent DOS (10MB) & get data from form
			r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)
			if err := r.ParseMultipartForm(0); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				s.respond(w, err.Error(), 2)
				return
			}
			// move form file to temp dir
			file, err := s.fileDB.uploadFile(w, r, userResponse.user)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				s.respond(w, err.Error(), 2)
				return
			}

			// html details form response
			templateData := struct {
				UploadedFile File
			}{
				file,
			}

			result := s.completeTemplate("/dynamic/templates/upload_form.html", templateData)
			if result == "" {
				w.WriteHeader(http.StatusBadRequest)
				s.respond(w, err.Error(), 1)
				return
			}
			s.respond(w, string(result), 3)

		// delete a file from user temp dir
		case "temp_delete":
			if err := r.ParseForm(); err != nil {
				s.respond(w, err.Error(), 2)
				return
			}

			// remove file
			response := s.fileDB.performAccessRequest(FileAccessRequest{operation: "deleteFile", target: r.Form.Get("file")})
			if response.err != nil {
				s.respond(w, response.err.Error(), 2)
				return
			}

			s.respond(w, "success", 3)

		// move temp file to content dir (allow global user access)
		case "publish":
			if err := r.ParseForm(); err != nil {
				s.respond(w, err.Error(), 2)
				return
			}

			// process tags and people fields
			tags := ProcessInputList(r.Form.Get("tags-input"), ",", true)
			people := ProcessInputList(r.Form.Get("people-input"), ",", true)
			metaData := MetaData{Description: r.Form.Get("description-input"), Tags: tags, People: people}

			// validate form field lengths
			if len(tags) == 0 {
				s.respond(w, "no_tags", 3)
				return
			}
			if len(people) == 0 {
				s.respond(w, "no_people", 3)
				return
			}

			// add file to DB & move from db/temp dir to db/content dir
			response := s.fileDB.performAccessRequest(FileAccessRequest{operation: "publishFile", UUID: r.Form.Get("file"), fileMetadata: metaData})
			if response.err != nil {
				s.respond(w, response.err.Error(), 2)
				return
			}

			// success
			s.respond(w, "success", 3)
		}
	}
}

// Write a HTTP response to connection.
func (s *HTTPServer) respond(w http.ResponseWriter, response string, logLevel int) {
	config.Log(response, logLevel)

	// write
	if _, err := fmt.Fprintf(w, "%v\n", response); err != nil {
		log.Println(err)
	}
}

// Replace variables in HTML templates with corresponding values in TemplateData.
func (s *HTTPServer) completeTemplate(filePath string, data interface{}) (result template.HTML) {
	filePath = config.rootPath + filePath

	// load HTML template from disk
	htmlTemplate, err := ioutil.ReadFile(filePath)
	if err != nil {
		config.Log(err.Error(), 1)
		return
	}

	// parse HTML template & register template functions
	templateParsed, err := template.New("t").Funcs(template.FuncMap{
		"formatEpoch": func(epoch int64) string {
			t := time.Unix(epoch, 0)
			return t.Format("02/01/2006 [15:04]")
		},
	}).Parse(string(htmlTemplate))
	if err != nil {
		config.Log(err.Error(), 1)
		return
	}

	// perform template variable replacement
	buffer := new(bytes.Buffer)
	if err = templateParsed.Execute(buffer, data); err != nil {
		config.Log(err.Error(), 1)
		return
	}

	return template.HTML(buffer.String())
}

// Gracefully stop the server and save DB to file.
func (s *HTTPServer) Stop() {
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	if err := s.server.Shutdown(ctx); err != nil {
		log.Println(err)
	}

	response := s.fileDB.performAccessRequest(FileAccessRequest{operation: "serialize"})
	if response.err != nil {
		log.Println(response.err)
	}
}
