// Package memoryshare implements a memory sharing service.
package memoryshare

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

var config *Config

// A server providing file sharing and access related services.
type ServerBase struct {
	fileDB         *FileDB
	startTimestamp int64
}

// Initialise servers.
func NewServerBase(conf *Config) (err error, httpServer HTTPServer) {
	config = conf

	// create new file DB
	fileDB, err := NewFileDB(config.RootPath + "/db")
	if err != nil {
		Critical.Logf("Server error: %v", err)
		return
	}

	// create new user DB
	userDB, err := NewUserDB(config.RootPath + "/db")
	if err != nil {
		Critical.Logf("Server error: %v", err)
		return
	}

	// get port from config
	port, err := config.GetInt("http_port")
	if err != nil {
		port = 8000
	}

	// start http server
	httpServer = HTTPServer{
		host: "localhost",
		port: port,
		ServerBase: ServerBase{
			fileDB:         fileDB,
			startTimestamp: time.Now().Unix(),
		},
		userDB: userDB,
	}

	// set host (allow_public_webapp)
	if config.GetBool("allow_public_webapp") {
		httpServer.host = "0.0.0.0"
	}
	// set port (http_port)
	if httpServer.port, err = config.GetInt("http_port"); err != nil {
		Critical.Logf("Server error: %v", "invalid port value found in config file - using default")
	}
	// set maxFileUploadSize (default 50MB)
	if httpServer.maxFileUploadSize, err = config.GetInt("max_file_upload_size"); err != nil {
		httpServer.maxFileUploadSize = 50
	}
	httpServer.maxFileUploadSize *= 1024 * 1024

	httpServer.Start()
	return
}

type HTTPServer struct {
	host string
	port int
	ServerBase
	server            *http.Server
	userDB            *UserDB
	maxFileUploadSize int
}

// Start listening for HTTP requests.
func (s *HTTPServer) Start() {
	// define HTTP routes
	router := mux.NewRouter()

	// user auth
	router.HandleFunc("/login", s.authHandler(s.loginHandler)).Methods(http.MethodGet, http.MethodPost)
	router.HandleFunc("/logout", s.authHandler(s.logoutHandler)).Methods(http.MethodGet)
	router.HandleFunc("/reset", s.resetHandler).Methods(http.MethodGet)
	router.HandleFunc("/reset/{type}", s.resetHandler).Methods(http.MethodPost)
	// list all users
	router.HandleFunc("/users", s.authHandler(s.viewUsersHandler)).Methods(http.MethodGet)
	// single user
	router.HandleFunc("/user", s.authHandler(s.manageUserHandler)).Methods(http.MethodPost)
	router.HandleFunc("/user/{username}", s.authHandler(s.manageUserHandler)).Methods(http.MethodGet, http.MethodPost)
	// memory/file data viewing
	router.HandleFunc("/", s.authHandler(s.viewMemoriesHandler)).Methods(http.MethodGet)
	router.HandleFunc("/memory/{fileUUID}", s.authHandler(s.viewMemoriesHandler)).Methods(http.MethodGet) // passive route, JS utilises fileUUID
	router.HandleFunc("/search", s.authHandler(s.searchMemoriesHandler)).Methods(http.MethodGet)
	router.HandleFunc("/data", s.authHandler(s.getDataHandler)).Methods(http.MethodGet, http.MethodPost)
	// upload
	router.HandleFunc("/upload", s.authHandler(s.uploadHandler)).Methods(http.MethodGet)
	router.HandleFunc("/upload/{type}", s.authHandler(s.uploadHandler)).Methods(http.MethodPost)
	// static uploaded file server
	staticFileHandler := http.StripPrefix("/static/", http.FileServer(http.Dir(config.RootPath+"/static/")))
	router.Handle(`/static/{rest:[a-zA-Z0-9=\-\/._]+}`, s.fileServerAuthHandler(staticFileHandler))
	// temp uploaded file server
	tempFileHandler := http.StripPrefix("/temp_uploaded/", http.FileServer(http.Dir(config.RootPath+"/db/temp/")))
	router.Handle(`/temp_uploaded/{user_id:[a-zA-Z0-9=\-_]+}/{file:[a-zA-Z0-9=\-\/._]+}`, s.fileServerAuthHandler(tempFileHandler))

	s.server = &http.Server{
		Handler:      router,
		Addr:         s.host + ":" + fmt.Sprintf("%d", s.port),
		WriteTimeout: 5 * time.Second,
		ReadTimeout:  5 * time.Second,
	}

	// listen for HTTP requests
	Info.Logf("starting HTTP server on port %d", s.port)

	go func(server *http.Server) {
		if err := server.ListenAndServe(); err != nil {
			log.Println(err)
		}
	}(s.server)
}

// Request handler wrapper for auth.
func (s *HTTPServer) authHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		Incoming.Logf("%v -> %v [%v]", r.Host, r.URL, r.Method)

		// authenticate
		authorised, authErr := s.userDB.authenticateUser(r)

		// file servers
		// prevent dir listings
		if r.URL.String() != "/" && strings.HasSuffix(r.URL.String(), "/") {
			s.respond(w, "404 page not found")
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

			sessionUser, err := s.userDB.getSessionUser(r)
			if sessionUser.Username != vars["user_id"] || err != nil {
				s.respond(w, "404 page not found")
				return
			}
		}

		// if already logged in, redirect these page requests
		if r.URL.String() == "/login" {
			if authorised {
				if r.Method == http.MethodGet {
					http.Redirect(w, r, "/", 302)
				} else {
					s.respond(w, "already authenticated")
				}
			} else {
				h(w, r)
				return
			}
		}
		// if auth failed (error or wrong password)
		if authErr != nil || authorised == false {
			if authErr != nil {
				Input.Log(authErr)
			}

			if r.Method == http.MethodGet {
				http.Redirect(w, r, "/login", 302)
			} else {
				s.respond(w, "unauthorised")
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
			config.Get("brand_name"),
			"",
			"",
		}

		templateData.FooterHTML = s.completeTemplate("/dynamic/templates/footers/login_footer.html", templateData)
		templateData.ContentHTML = s.completeTemplate("/dynamic/templates/reset_password.html", templateData)
		result := s.completeTemplate("/dynamic/templates/main.html", templateData)

		s.respond(w, result)

	// submit password reset request
	case http.MethodPost:
		r.ParseForm()
		vars := mux.Vars(r)
		fmt.Println(vars)

		s.respond(w, "not yet implemented")
		return

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
		msg.SetHeader("From", "admin@memoryshare.com") // config.Get("display_email_addr")
		msg.SetHeader("To", "jemgunay@yahoo.co.uk")
		msg.SetHeader("Subject", config.Get("brand_name")+": Password Reset")
		msg.SetBody("text/html", msgBody)

		/*port, err := strconv.Atoi(config.Get("core_email_port"))
		if err != nil {
			s.respond(w, "error", 2)
			return
		}*/

		//d := gomail.NewDialer(config.Get("core_email_server"), port, config.Get("core_email_addr"), config.Get("core_email_password"))

		d := gomail.NewDialer("smtp.gmail.com", 465, config.Get("core_email_addr"), config.Get("core_email_password"))

		// Send the email to Bob, Cora and Dan.
		if err := d.DialAndSend(msg); err != nil {
			log.Println(err)
			s.respond(w, "error")
			return
		}

		s.respond(w, "success")
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
			config.Get("brand_name"),
			"",
			"",
		}

		templateData.FooterHTML = s.completeTemplate("/dynamic/templates/footers/login_footer.html", templateData)
		templateData.ContentHTML = s.completeTemplate("/dynamic/templates/login.html", templateData)
		result := s.completeTemplate("/dynamic/templates/main.html", templateData)

		s.respond(w, result)

	// submit login request
	case http.MethodPost:
		success, err := s.userDB.loginUser(w, r)

		switch {
		case err != nil:
			s.respond(w, "error")
		case success:
			s.respond(w, "success")
		default:
			s.respond(w, "unauthorised")
		}
	}
}

// Handle logout.
func (s *HTTPServer) logoutHandler(w http.ResponseWriter, r *http.Request) {
	if err := s.userDB.logoutUser(w, r); err != nil {
		s.respond(w, "error")
		return
	}
	http.Redirect(w, r, "/login", 302)
}

// Process HTTP view users request.
func (s *HTTPServer) viewUsersHandler(w http.ResponseWriter, r *http.Request) {
	// get session user
	sessionUser, err := s.userDB.getSessionUser(r)
	if err != nil {
		Critical.Log(err)
		s.respond(w, "error")
		return
	}

	// get list of all users
	allUsers := s.userDB.getUsers()

	// HTML template data
	templateData := struct {
		Title       string
		BrandName   string
		SessionUser User
		Users       []User
		NavbarHTML  template.HTML
		NavbarFocus string
		FooterHTML  template.HTML
		ContentHTML template.HTML
	}{
		"Users",
		config.Get("brand_name"),
		sessionUser,
		allUsers,
		"",
		"users",
		"",
		"",
	}

	templateData.NavbarHTML = s.completeTemplate("/dynamic/templates/navbar.html", templateData)
	templateData.FooterHTML = s.completeTemplate("/dynamic/templates/footers/search_footer.html", templateData)
	templateData.ContentHTML = s.completeTemplate("/dynamic/templates/users_list.html", templateData)
	result := s.completeTemplate("/dynamic/templates/main.html", templateData)

	s.respond(w, result)
}

// Process a single user request.
func (s *HTTPServer) manageUserHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			Critical.Log(err)
			s.respond(w, "error")
			return
		}
	}

	// get session user
	sessionUser, err := s.userDB.getSessionUser(r)
	if err != nil {
		Critical.Log(err)
		s.respond(w, "error")
		return
	}

	if vars["username"] != "" {
		// get user corresponding with
		user, err := s.userDB.getUserByUsername(vars["username"])
		if err != nil {
			s.respond(w, err)
			return
		}

		switch r.Method {
		// get specific user details -> /user/{username}
		case http.MethodGet:

			// HTML template data
			templateData := struct {
				Title       string
				BrandName   string
				SessionUser User
				User        User
				Files       []File // favourite files
				NavbarHTML  template.HTML
				NavbarFocus string
				FooterHTML  template.HTML
				ContentHTML template.HTML
				FilesHTML   template.HTML
				Status      string
			}{
				"Profile",
				config.Get("brand_name"),
				sessionUser,
				user,
				[]File{},
				"",
				"users",
				"",
				"",
				"",
				"ok",
			}

			// get favourite memories
			for fileUUID := range user.FavouriteFileUUIDs {
				file, ok := s.fileDB.Published.Get(fileUUID)
				if ok && file.UUID != "" {
					templateData.Files = append(templateData.Files, file)
				}
			}

			var filesHTMLTarget = "/dynamic/templates/files_list_tiled.html"
			if len(templateData.Files) == 0 {
				filesHTMLTarget = "/static/templates/no_match_favourites.html"
			}

			// set navbarfocus based on if viewed user IS the session user
			if vars["username"] == sessionUser.Username {
				templateData.NavbarFocus = "user"
			}
			templateData.FilesHTML = s.completeTemplate(filesHTMLTarget, templateData)
			templateData.NavbarHTML = s.completeTemplate("/dynamic/templates/navbar.html", templateData)
			templateData.FooterHTML = s.completeTemplate("/dynamic/templates/footers/search_footer.html", templateData)
			templateData.ContentHTML = s.completeTemplate("/dynamic/templates/user_content.html", templateData)
			result := s.completeTemplate("/dynamic/templates/main.html", templateData)

			s.respond(w, result)

		// update specific user details -> /user/{username}
		case http.MethodPost:
			// check auth or admin privs first
			s.respond(w, "not yet implemented")
		}
	} else {
		switch r.Method {
		// add a file to a user's favourites -> /user
		case http.MethodPost:
			switch r.Form.Get("operation") {
			case "favourite":
				state, _ := strconv.ParseBool(r.Form.Get("state"))

				err := s.userDB.setFavourite(sessionUser.Username, r.Form.Get("fileUUID"), state)
				if err != nil {
					s.respond(w, err)
					return
				}

				if state {
					s.respond(w, "favourite_successfully_added")
				} else {
					s.respond(w, "favourite_successfully_removed")
				}
			}

		}

	}

	return
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
// URL params: [desc, start_date, end_date, file_types, tags, people, format(json/html_tiled/html_detailed), pretty, results_per_page(0=all)]
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
	fileResults := s.fileDB.search(searchReq)

	// respond with JSON or HTML?
	if q.Get("format") == "html_tiled" || q.Get("format") == "html_detailed" {
		// HTML formatted response
		templateData := struct {
			Files  []File
			Status string
		}{
			fileResults.Files,
			fileResults.state,
		}
		// determine which template format to use
		templateFile := "/dynamic/templates/files_list_detailed.html"
		if q.Get("format") == "html_tiled" {
			templateFile = "/dynamic/templates/files_list_tiled.html"
		}

		if len(fileResults.Files) == 0 {
			templateFile = "/dynamic/templates/no_match.html"
		}

		filesListResult := s.completeTemplate(templateFile, templateData)
		s.respond(w, filesListResult)
		return
	}

	// JSON formatted response
	prettyPrint, _ := strconv.ParseBool(q.Get("pretty"))
	filesJSON := ToJSON(fileResults, prettyPrint)

	s.respond(w, filesJSON)
}

// Get specific JSON data such as all tags & people.
// URL params (data is returned for metadata types included in the fetch param): ?fetch=tags,people,file_types,dates
func (s *HTTPServer) getDataHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	// get groups of meta data
	case http.MethodGet:
		q := r.URL.Query()
		resultsList := make(map[string][]string)

		metaDataTypes := ProcessInputList(q.Get("fetch"), ",", true)
		for _, dataType := range metaDataTypes {
			resultsList[dataType] = s.fileDB.getMetaData(dataType)
		}

		// parse query result to json
		response, err := json.Marshal(resultsList)
		if err != nil {
			Critical.Log(err)
			s.respond(w, "error")
			return
		}
		s.respond(w, response)

	// get specific item by UUID (a file or user): ?UUID=X|random&type=file|user&format=html|json_pretty|json)
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			Critical.Log(err)
			s.respond(w, "error")
			return
		}

		// check UUID provided
		targetUUID := r.Form.Get("UUID")
		if targetUUID == "" {
			s.respond(w, "no_UUID_provided")
			return
		}

		switch r.Form.Get("type") {
		// get specific file
		case "file":
			if targetUUID == "random" {
				randomFile, err := s.fileDB.getRandomFile()
				if err != nil {
					s.respond(w, "no files found")
					return
				}

				targetUUID = randomFile.UUID
			}

			// fetch file from DB
			file, ok := s.fileDB.Published.Get(targetUUID)
			if ok && file.UUID == "" {
				s.respond(w, "no_UUID_match")
				return
			}

			// pretty print
			switch r.Form.Get("format") {
			case "html":
				// get user
				user, err := s.userDB.getUserByUsername(file.UploaderUsername)
				if err != nil {
					s.respond(w, "error finding corresponding user")
					return
				}
				isFavourite := user.FavouriteFileUUIDs[file.UUID]

				templateData := struct {
					File
					User
					IsFavourite bool
				}{
					file,
					user,
					isFavourite,
				}

				result := s.completeTemplate("/dynamic/templates/file_content_overlay.html", templateData)
				s.respond(w, result)

			case "json_pretty":
				s.respond(w, ToJSON(file, true))

			case "json":
				fallthrough
			default:
				s.respond(w, ToJSON(file, false))
			}

		// get specific user from DB
		case "user":
			// fetch file from DB
			user, err := s.userDB.getUserByUsername(targetUUID)
			if err != nil {
				s.respond(w, "error finding corresponding user")
				return
			}
			if user.Username == "" {
				s.respond(w, "no_UUID_match")
				return
			}

			switch r.Form.Get("format") {
			case "html":
				s.respond(w, "html_not_supported")
			case "json_pretty":
				s.respond(w, ToJSON(user, true))
			case "json":
				fallthrough
			default:
				s.respond(w, ToJSON(user, false))
			}

		default:
			s.respond(w, "no_type_provided")
		}
	}
}

// Process HTTP view files request.
func (s *HTTPServer) viewMemoriesHandler(w http.ResponseWriter, r *http.Request) {
	// get session user
	sessionUser, err := s.userDB.getSessionUser(r)
	if err != nil {
		Critical.Log(err)
		s.respond(w, "error")
		return
	}

	switch r.Method {
	case http.MethodGet:
		// HTML template data
		templateData := struct {
			Title       string
			BrandName   string
			SessionUser User
			NavbarHTML  template.HTML
			NavbarFocus string
			FooterHTML  template.HTML
			ContentHTML template.HTML
		}{
			"Memories",
			config.Get("brand_name"),
			sessionUser,
			"",
			"search",
			"",
			"",
		}

		templateData.NavbarHTML = s.completeTemplate("/dynamic/templates/navbar.html", templateData)
		templateData.FooterHTML = s.completeTemplate("/dynamic/templates/footers/search_footer.html", templateData)
		templateData.ContentHTML = s.completeTemplate("/dynamic/templates/search.html", templateData)
		result := s.completeTemplate("/dynamic/templates/main.html", templateData)

		s.respond(w, result)
	}
}

// Process HTTP file upload request.
func (s *HTTPServer) uploadHandler(w http.ResponseWriter, r *http.Request) {
	// get session user
	sessionUser, err := s.userDB.getSessionUser(r)
	if err != nil {
		Critical.Log(err)
		s.respond(w, "error")
		return
	}

	vars := mux.Vars(r)

	switch r.Method {
	case http.MethodGet:
		// fetch upload page
		templateData := struct {
			Title             string
			BrandName         string
			SessionUser       User
			NavbarHTML        template.HTML
			NavbarFocus       string
			FooterHTML        template.HTML
			UploadHTML        template.HTML
			ContentHTML       template.HTML
			UploadFormsHTML   template.HTML
			MaxFileUploadSize int64
		}{
			"Upload",
			config.Get("brand_name"),
			sessionUser,
			"",
			"upload",
			"",
			"",
			"",
			"",
			int64(s.maxFileUploadSize),
		}

		// get all uploaded/temp files for session user
		files := s.fileDB.getFilesByUser(sessionUser.Username, UPLOADED)

		// generate upload description forms for each unpublished image
		for _, f := range files {
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

		s.respond(w, result)

	// file upload
	case http.MethodPost:
		// upload file to temp dir under user UUID subdir ready for processing (only uploading user can access)
		switch vars["type"] {
		case "temp":
			// limit request size to prevent DOS & get data from form
			r.Body = http.MaxBytesReader(w, r.Body, int64(s.maxFileUploadSize))
			if err := r.ParseMultipartForm(0); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				Input.Log(err.Error())
				s.respond(w, "error")
				return
			}
			// move form file to temp dir
			uploadedFile, err := s.fileDB.uploadFile(r, sessionUser)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				s.respond(w, err)
				return
			}

			// html details form response
			templateData := struct {
				UploadedFile File
			}{
				uploadedFile,
			}

			result := s.completeTemplate("/dynamic/templates/upload_form.html", templateData)
			if result == "" {
				w.WriteHeader(http.StatusBadRequest)
				s.respond(w, "error")
				return
			}
			s.respond(w, result)

		// delete a file from user temp dir
		case "temp_delete":
			if err := r.ParseForm(); err != nil {
				Input.Log(err)
				s.respond(w, "error")
				return
			}

			// remove file
			if err := s.fileDB.deleteFile(r.Form.Get("fileUUID")); err != nil {
				s.respond(w, err)
				return
			}

			s.respond(w, "success")

		// move temp file to content dir (allow global user access)
		case "publish":
			if err := r.ParseForm(); err != nil {
				Input.Log(err)
				s.respond(w, "error")
				return
			}

			// process tags and people fields
			desc := r.Form.Get("description-input")
			tags := ProcessInputList(r.Form.Get("tags-input"), ",", true)
			people := ProcessInputList(r.Form.Get("people-input"), ",", true)
			metaData := MetaData{Description: desc, Tags: tags, People: people}

			// validate form field lengths
			if len(desc) == 0 {
				s.respond(w, "no_description")
				return
			}
			if len(tags) == 0 {
				s.respond(w, "no_tags")
				return
			}
			if len(people) == 0 {
				s.respond(w, "no_people")
				return
			}

			// add file to DB & move from db/temp dir to db/content dir
			if err := s.fileDB.publishFile(r.Form.Get("fileUUID"), metaData); err != nil {
				s.respond(w, err)
				return
			}

			// success
			s.respond(w, "success")
		}
	}
}

// Write a HTTP response to a ResponseWriter with a status code of 200.
func (s *HTTPServer) respond(w http.ResponseWriter, response interface{}) {
	s.respondStatus(w, response, http.StatusOK)
}

// Write a HTTP response to a ResponseWriter with a specific status code.
func (s *HTTPServer) respondStatus(w http.ResponseWriter, response interface{}, statusCode int) {
	// type cast response into string
	switch response.(type) {
	case template.HTML:
		response = string(response.(template.HTML))
	case []byte:
		response = string(response.([]byte))
	}
	Output.Log(response)

	w.WriteHeader(statusCode)

	// write response
	if _, err := fmt.Fprint(w, response); err != nil {
		Critical.Log(err)
	}
}

// Replace variables in HTML templates with corresponding values in TemplateData.
func (s *HTTPServer) completeTemplate(filePath string, data interface{}) (result template.HTML) {
	filePath = config.RootPath + filePath

	// load HTML template from disk
	htmlTemplate, err := ioutil.ReadFile(filePath)
	if err != nil {
		Critical.Log(err)
		return
	}

	// parse HTML template & register template functions
	templateParsed, err := template.New("t").Funcs(template.FuncMap{
		"formatEpoch": func(epoch int64) string {
			t := time.Unix(0, epoch)
			return t.Format("02/01/2006 [15:04]")
		},
		"formatByteCount": func(bytes int64, si bool) string {
			return FormatByteCount(bytes, si)
		},
		"toTitleCase": strings.Title,
	}).Parse(string(htmlTemplate))

	if err != nil {
		Critical.Log(err)
		return
	}

	// perform template variable replacement
	buffer := new(bytes.Buffer)
	if err = templateParsed.Execute(buffer, data); err != nil {
		Critical.Log(err)
		return
	}

	return template.HTML(buffer.String())
}

// Gracefully stop the server and save DB to file.
func (s *HTTPServer) Stop() {
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	if err := s.server.Shutdown(ctx); err != nil {
		Info.Log(err)
	}
}
