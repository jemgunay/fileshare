// Package memoryshare implements a memory sharing service.
package memoryshare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"gopkg.in/gomail.v2"
)

var config *Config

// Server wraps both a HTTP server and the file & user databases.
type Server struct {
	startTimestamp    int64
	server            *http.Server
	host              string
	port              int
	fileDB            *FileDB
	maxFileUploadSize int
	userDB            *UserDB
}

// NewServer initialises the file & user databases, then launches the HTTP server.
func NewServer(conf *Config) (httpServer Server, err error) {
	config = conf

	// create new file DB
	fileDB, err := NewFileDB(config.rootPath + "/db")
	if err != nil {
		Critical.Logf("Server error: %v", err)
		return
	}

	// create new user DB
	userDB, err := NewUserDB(config.rootPath + "/db")
	if err != nil {
		Critical.Logf("Server error: %v", err)
		return
	}

	// start http server
	httpServer = Server{
		host:              "localhost",
		port:              config.HTTPPort,
		fileDB:            fileDB,
		startTimestamp:    time.Now().Unix(),
		userDB:            userDB,
		maxFileUploadSize: config.MaxFileUploadSize,
	}

	// set host (allow_public_webapp)
	if config.AllowPublicWebApp {
		httpServer.host = "0.0.0.0"
	}

	httpServer.Start()
	return
}

// Start starts listening for HTTP requests.
func (s *Server) Start() {
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
	Info.Logf("starting HTTP server on port %d", s.port)

	go func(server *http.Server) {
		if err := server.ListenAndServe(); err != nil {
			Critical.Log(err)
		}
	}(s.server)
}

// authHandler is a HTTP handler wrapper which authenticates requests.
func (s *Server) authHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		Incoming.Logf("%v -> [%v] %v", r.Host, r.Method, r.URL)

		// authenticate
		authorised, authErr := s.userDB.AuthenticateUser(r)

		// file servers
		// prevent dir listings
		if r.URL.String() != "/" && strings.HasSuffix(r.URL.String(), "/") {
			s.RespondStatus(w, r, "404 page not found", http.StatusNotFound)
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

			sessionUser, err := s.userDB.GetSessionUser(r)
			if sessionUser.Username != vars["user_id"] || err != nil {
				s.RespondStatus(w, r, "404 page not found", http.StatusNotFound)
				return
			}
		}

		// if already logged in, redirect these page requests
		if r.URL.String() == "/login" {
			if authorised {
				if r.Method == http.MethodGet {
					http.Redirect(w, r, "/", http.StatusFound)
				} else {
					s.Respond(w, r, "already authenticated")
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
				s.Respond(w, r, "unauthorised")
			}
			return
		}

		// continue to call handler
		h(w, r)
	}
}

// fileServerAuthHandler is a file server authentication wrapper.
func (s *Server) fileServerAuthHandler(h http.Handler) http.Handler {
	return s.authHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
	}))
}

// resetHandler is a HTTP handler which manages user password reset requests.
func (s *Server) resetHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	// fetch reset form
	case http.MethodGet:
		// HTML template data
		templateData := struct {
			Title       string
			BrandName   string
			FooterHTML  template.HTML
			ContentHTML template.HTML
		}{
			"Reset Password",
			config.ServiceName,
			"",
			"",
		}

		templateData.FooterHTML = s.CompleteTemplate("/dynamic/templates/footers/login_footer.html", templateData)
		templateData.ContentHTML = s.CompleteTemplate("/dynamic/templates/reset_password.html", templateData)
		result := s.CompleteTemplate("/dynamic/templates/main.html", templateData)

		s.Respond(w, r, result)

	// submit password reset request
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			Critical.Log(err)
			s.Respond(w, r, "error")
			return
		}

		recipientEmail := r.FormValue("email")

		// perform password reset & email sending in the background
		go func() {
			// set temp password if user exists (don't inform user of failed reset attempt to prevent address brute forcing)
			tempPass, err := s.userDB.SetTempPassword(recipientEmail)
			if err != nil {
				return
			}

			// construct new email with randomly generated temp password
			msgBody := fmt.Sprintf("<html><body><p>This is your temporary one time use password: <br><br><b>%v", tempPass)
			msgBody += "</b><br><br>Use it to log in and change your password. It will expire in one hour.</p></body></html>"

			msg := gomail.NewMessage()
			msg.SetAddressHeader("From", config.EmailDisplayAddr, "Memory Share")
			msg.SetHeader("To", recipientEmail)
			msg.SetHeader("Subject", config.ServiceName+": Password Reset")
			msg.SetBody("text/html", msgBody)

			d := gomail.NewDialer(config.EmailServer, config.EmailPort, config.EmailAddr, config.EmailPass)
			//d.TLSConfig = &tls.Config{InsecureSkipVerify: true}

			// send email
			if err := d.DialAndSend(msg); err != nil {
				Critical.Log(err)
				return
			}
		}()

		s.Respond(w, r, "success")
		return

	}
}

// loginHandler is a HTTP handler which manages user logins.
func (s *Server) loginHandler(w http.ResponseWriter, r *http.Request) {
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
			config.ServiceName,
			"",
			"",
		}

		templateData.FooterHTML = s.CompleteTemplate("/dynamic/templates/footers/login_footer.html", templateData)
		templateData.ContentHTML = s.CompleteTemplate("/dynamic/templates/login.html", templateData)
		result := s.CompleteTemplate("/dynamic/templates/main.html", templateData)

		s.Respond(w, r, result)

	// submit login request
	case http.MethodPost:
		success, err := s.userDB.LoginUser(w, r)
		fmt.Println(err)
		switch {
		case err != nil:
			s.Respond(w, r, "error")
		case success:
			s.Respond(w, r, "success")
		default:
			s.Respond(w, r, "unauthorised")
		}
	}
}

// logoutHandler is a HTTP handler which manages user logouts.
func (s *Server) logoutHandler(w http.ResponseWriter, r *http.Request) {
	if err := s.userDB.LogoutUser(w, r); err != nil {
		s.Respond(w, r, "error")
		return
	}
	http.Redirect(w, r, "/login", 302)
}

// viewUsersHandler is a HTTP handler which provides a view of all service users.
func (s *Server) viewUsersHandler(w http.ResponseWriter, r *http.Request) {
	// get session user
	sessionUser, err := s.userDB.GetSessionUser(r)
	if err != nil {
		Critical.Log(err)
		s.Respond(w, r, "error")
		return
	}

	// get list of all users
	allUsers := s.userDB.GetUsers()

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
		config.ServiceName,
		sessionUser,
		allUsers,
		"",
		"users",
		"",
		"",
	}

	templateData.NavbarHTML = s.CompleteTemplate("/dynamic/templates/navbar.html", templateData)
	templateData.FooterHTML = s.CompleteTemplate("/dynamic/templates/footers/search_footer.html", templateData)
	templateData.ContentHTML = s.CompleteTemplate("/dynamic/templates/users_list.html", templateData)
	result := s.CompleteTemplate("/dynamic/templates/main.html", templateData)

	s.Respond(w, r, result)
}

// manageUserHandler is a HTTP handler which manages requests relating to a single user.
func (s *Server) manageUserHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			Critical.Log(err)
			s.Respond(w, r, "error")
			return
		}
	}

	// get session user
	sessionUser, err := s.userDB.GetSessionUser(r)
	if err != nil {
		Critical.Log(err)
		s.Respond(w, r, "error")
		return
	}

	if vars["username"] != "" {
		// get user corresponding with
		user, err := s.userDB.GetUserByUsername(vars["username"])
		if err != nil {
			s.Respond(w, r, err)
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
				Status      string
			}{
				"Profile",
				config.ServiceName,
				sessionUser,
				user,
				[]File{},
				"",
				"users",
				"",
				"",
				"ok",
			}

			// set navbar focus based on if viewed user IS the session user
			if vars["username"] == sessionUser.Username {
				templateData.NavbarFocus = "user"
			}
			templateData.NavbarHTML = s.CompleteTemplate("/dynamic/templates/navbar.html", templateData)
			templateData.FooterHTML = s.CompleteTemplate("/dynamic/templates/footers/search_footer.html", templateData)
			templateData.ContentHTML = s.CompleteTemplate("/dynamic/templates/user_content.html", templateData)
			result := s.CompleteTemplate("/dynamic/templates/main.html", templateData)

			s.Respond(w, r, result)

			// update specific user details -> /user/{username}
		case http.MethodPost:
			// check auth or admin privs first
			s.Respond(w, r, "not yet implemented")
		}
		return
	}

	switch r.Method {
	// add a file to a user's favourites -> /user
	case http.MethodPost:
		switch r.Form.Get("operation") {
		case "favourite":
			state, _ := strconv.ParseBool(r.Form.Get("state"))

			err := s.userDB.SetFavourite(sessionUser.Username, r.Form.Get("fileUUID"), state)
			if err != nil {
				s.Respond(w, r, err)
				return
			}

			if state {
				s.Respond(w, r, "favourite_successfully_added")
			} else {
				s.Respond(w, r, "favourite_successfully_removed")
			}
		}
	}

	return
}

// SearchRequest is a container for all of the search criteria required by the FileDB's search function.
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

// searchMemoriesHandler is a HTTP handler which processes & validates input search criteria then writes formatted
// search results. URL params: {
//     desc,
//     start_date,
//     end_date,
//     file_types (comma separated list),
//     tags (comma separated list),
//     people (comma separated list),
//     format = ["json", "html_tiled", "html_detailed"],
//     pretty = [true, false],
//     results_per_page (0=all memories)
// }
func (s *Server) searchMemoriesHandler(w http.ResponseWriter, r *http.Request) {
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
	fileResults := s.fileDB.Search(searchReq)

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

		filesListResult := s.CompleteTemplate(templateFile, templateData)
		s.Respond(w, r, filesListResult)
		return
	}

	// JSON formatted response
	prettyPrint, _ := strconv.ParseBool(q.Get("pretty"))
	filesJSON := ToJSON(fileResults, prettyPrint)

	s.Respond(w, r, filesJSON)
}

// getDataHandler is a HTTP handler which retrieves specific JSON metadata or specific memory data.
// GET URL params: {
//     fetch = tags,people,file_types,dates (comma separated list, each is optional),
// }
// POST JSON params: {
//     type = ["file", "user"]
//     UUID = "random" or a specific file UUID (used only when type == "file"),
//     username (used only when type == "user"),
//     format = ["json", "html_tiled", "html_detailed"],
// }
func (s *Server) getDataHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	// get groups of meta data
	case http.MethodGet:
		s.processMetadataRequest(w, r)

	// get specific item by UUID (a file or user): ?UUID=X|random&type=file|user&format=html|json_pretty|json)
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			Critical.Log(err)
			s.Respond(w, r, "error")
			return
		}

		switch r.Form.Get("type") {
		// get specific file from DB
		case "file":
			s.processFileRequest(w, r)

		// get specific user from DB
		case "user":
			// check if username provided
			targetUsername := r.Form.Get("username")
			if targetUsername == "" {
				s.Respond(w, r, "no_username_provided")
				return
			}

			// fetch User from DB
			user, err := s.userDB.GetUserByUsername(targetUsername)
			if err != nil {
				s.Respond(w, r, "no_username_match")
				return
			}

			switch r.Form.Get("format") {
			case "json_pretty":
				s.Respond(w, r, ToJSON(user, true))
			default:
				s.Respond(w, r, ToJSON(user, false))
			}

		default:
			s.Respond(w, r, "no_type_provided")
		}
	}
}

// processMetadataRequest processes a MetaData fetch request.
func (s *Server) processMetadataRequest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	resultsList := make(map[string][]string)

	metaDataTypes := ProcessInputList(q.Get("fetch"), ",", true)
	for _, dataType := range metaDataTypes {
		resultsList[dataType] = s.fileDB.GetMetaData(dataType)
	}

	// parse query result to json
	response, err := json.Marshal(resultsList)
	if err != nil {
		Critical.Log(err)
		s.Respond(w, r, "error")
		return
	}
	s.Respond(w, r, response)
}

// processFileRequest processes a request for a File by UUID.
func (s *Server) processFileRequest(w http.ResponseWriter, r *http.Request) {
	// check UUID provided
	targetUUID := r.Form.Get("UUID")
	if targetUUID == "" {
		s.Respond(w, r, "no_UUID_provided")
		return
	}

	if targetUUID == "random" {
		randomFile, err := s.fileDB.GetRandomFile()
		if err != nil {
			switch err {
			case FileDBEmptyError:
				s.Respond(w, r, "no_files_published")
			default:
				s.Respond(w, r, "random_error")
				Critical.Logf("%+v", err)
				return
			}
			Input.Log(err)
			return
		}

		targetUUID = randomFile.UUID
	}

	// fetch file from DB
	file, ok := s.fileDB.Published.Get(targetUUID)
	if ok && file.UUID == "" {
		s.Respond(w, r, "no_UUID_match")
		return
	}

	switch r.Form.Get("format") {
	case "html":
		// get user
		user, err := s.userDB.GetUserByUsername(file.UploaderUsername)
		if err != nil {
			s.Respond(w, r, "no_username_match")
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

		result := s.CompleteTemplate("/dynamic/templates/file_content_overlay.html", templateData)
		s.Respond(w, r, result)

	case "json_pretty":
		s.Respond(w, r, ToJSON(file, true))
	default:
		s.Respond(w, r, ToJSON(file, false))
	}
}

// viewMemoriesHandler is a HTTP handler which displays the memory view & search page.
func (s *Server) viewMemoriesHandler(w http.ResponseWriter, r *http.Request) {
	// get session user
	sessionUser, err := s.userDB.GetSessionUser(r)
	if err != nil {
		Critical.Log(err)
		s.Respond(w, r, "error")
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
			config.ServiceName,
			sessionUser,
			"",
			"search",
			"",
			"",
		}

		templateData.NavbarHTML = s.CompleteTemplate("/dynamic/templates/navbar.html", templateData)
		templateData.FooterHTML = s.CompleteTemplate("/dynamic/templates/footers/search_footer.html", templateData)
		templateData.ContentHTML = s.CompleteTemplate("/dynamic/templates/search.html", templateData)
		result := s.CompleteTemplate("/dynamic/templates/main.html", templateData)

		s.Respond(w, r, result)
	}
}

// uploadHandler is a HTTP handler which manages file upload requests and displaying the upload UI.
func (s *Server) uploadHandler(w http.ResponseWriter, r *http.Request) {
	// get session user
	sessionUser, err := s.userDB.GetSessionUser(r)
	if err != nil {
		Critical.Log(err)
		s.Respond(w, r, "error")
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
			config.ServiceName,
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
		files := s.fileDB.GetFilesByUser(sessionUser.Username, Uploaded)

		// generate upload description forms for each unpublished image
		for _, f := range files {
			uploadTemplateData := struct {
				UploadedFile File
			}{
				f,
			}

			result := s.CompleteTemplate("/dynamic/templates/upload_form.html", uploadTemplateData)
			templateData.UploadFormsHTML += result
		}

		templateData.NavbarHTML = s.CompleteTemplate("/dynamic/templates/navbar.html", templateData)
		templateData.FooterHTML = s.CompleteTemplate("/dynamic/templates/footers/upload_footer.html", templateData)
		templateData.ContentHTML = s.CompleteTemplate("/dynamic/templates/upload.html", templateData)
		result := s.CompleteTemplate("/dynamic/templates/main.html", templateData)

		s.Respond(w, r, result)

	// file upload
	case http.MethodPost:
		// upload file to temp dir under user UUID subdir ready for processing (only uploading user can access)
		switch vars["type"] {
		case "temp":
			// limit request size to prevent DOS & get data from form
			r.Body = http.MaxBytesReader(w, r.Body, int64(s.maxFileUploadSize))
			if err := r.ParseMultipartForm(0); err != nil {
				Input.Log(err)
				s.RespondStatus(w, r, "error", http.StatusBadRequest)
				return
			}

			// move form file to temp dir & create FileDB Uploaded entry
			uploadedFile, err := s.fileDB.UploadFile(r, sessionUser)
			if err != nil {
				if err, ok := err.(*FileExistsError); ok {
					s.RespondStatus(w, r, err.ConstructResponse(), http.StatusBadRequest)
					return
				}

				switch err {
				case InvalidFileError:
					s.RespondStatus(w, r, "invalid_file", http.StatusBadRequest)
				case UnsupportedFormatError:
					s.RespondStatus(w, r, "format_not_supported", http.StatusBadRequest)
				default:
					Critical.Logf("%+v", err)
					s.RespondStatus(w, r, "upload_error", http.StatusInternalServerError)
					return
				}
				Input.Log(err)
				return
			}

			// html details form response
			templateData := struct {
				UploadedFile File
			}{
				uploadedFile,
			}
			result := s.CompleteTemplate("/dynamic/templates/upload_form.html", templateData)
			if result == "" {
				s.RespondStatus(w, r, "error", http.StatusBadRequest)
				return
			}
			s.Respond(w, r, result)

		// delete a file from user temp dir
		case "temp_delete":
			if err := r.ParseForm(); err != nil {
				Input.Log(err)
				s.Respond(w, r, "error")
				return
			}

			// remove file
			if err := s.fileDB.DeleteFile(r.Form.Get("fileUUID")); err != nil {
				switch err {
				case FileNotFoundError:
					s.RespondStatus(w, r, "file_not_found", http.StatusBadRequest)
				case FileAlreadyDeletedError:
					s.RespondStatus(w, r, "file_already_deleted", http.StatusBadRequest)
				default:
					Critical.Logf("%+v", err)
					s.RespondStatus(w, r, "delete_error", http.StatusInternalServerError)
					return
				}
				Input.Log(err)
				return
			}

			s.Respond(w, r, "success")

		// move temp file to content dir (allow global user access)
		case "publish":
			if err := r.ParseForm(); err != nil {
				Input.Log(err)
				s.Respond(w, r, "error")
				return
			}

			// process tags and people fields
			desc := r.Form.Get("description-input")
			tags := ProcessInputList(r.Form.Get("tags-input"), ",", true)
			people := ProcessInputList(r.Form.Get("people-input"), ",", true)
			metaData := MetaData{Description: desc, Tags: tags, People: people}

			// validate form field lengths
			if len(desc) == 0 {
				s.Respond(w, r, "no_description")
				return
			}
			if len(tags) == 0 {
				s.Respond(w, r, "no_tags")
				return
			}
			if len(people) == 0 {
				s.Respond(w, r, "no_people")
				return
			}

			// add file to DB & move from db/temp dir to db/content dir
			if err := s.fileDB.PublishFile(r.Form.Get("fileUUID"), metaData); err != nil {
				switch err {
				case FileNotFoundError:
					s.Respond(w, r, "file_not_found")
				default:
					Critical.Logf("%+v", err)
					s.RespondStatus(w, r, "publish_error", http.StatusInternalServerError)
					return
				}
				Input.Log(err)
				return
			}

			// success
			s.Respond(w, r, "success")
		}
	}
}

// Respond writes a HTTP response to a ResponseWriter with a status code of 200.
func (s *Server) Respond(w http.ResponseWriter, r *http.Request, response interface{}) {
	s.RespondStatus(w, r, response, http.StatusOK)
}

// RespondStatus write a HTTP response to a ResponseWriter with a specific status code.
func (s *Server) RespondStatus(w http.ResponseWriter, r *http.Request, response interface{}, statusCode int) {
	defer r.Body.Close()

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

// functions that can be utilised by HTML templates
var templateFuncs = template.FuncMap{
	"formatEpoch": func(epoch int64) string {
		t := time.Unix(0, epoch)
		return t.Format("02/01/2006 [15:04]")
	},
	"formatByteCount": func(bytes int64, si bool) string {
		return FormatByteCount(bytes, si)
	},
	"toTitleCase": strings.Title,
}

// CompleteTemplate replaces variables in HTML templates with corresponding values in TemplateData.
func (s *Server) CompleteTemplate(filePath string, data interface{}) (result template.HTML) {
	filePath = config.rootPath + filePath

	// load HTML template from disk
	htmlTemplate, err := ioutil.ReadFile(filePath)
	if err != nil {
		Critical.Log(err)
		return
	}

	// parse HTML template & register template functions
	templateParsed, err := template.New("t").Funcs(templateFuncs).Parse(string(htmlTemplate))

	if err != nil {
		Critical.Log(err)
		return
	}

	// perform template variable replacement
	buffer := &bytes.Buffer{}
	if err = templateParsed.Execute(buffer, data); err != nil {
		Critical.Log(err)
		return
	}

	return template.HTML(buffer.String())
}

// Stop gracefully stops the HTTP server.
func (s *Server) Stop() context.CancelFunc {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := s.server.Shutdown(ctx); err != nil {
		Info.Log(err)
	}

	return cancel
}
