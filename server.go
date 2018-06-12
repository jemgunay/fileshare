// Package memoryshare implements a memory sharing service.
package memoryshare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/pkg/errors"
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

	// set host
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
	router.HandleFunc("/reset", s.authHandler(s.resetHandler)).Methods(http.MethodGet)
	router.HandleFunc("/reset/{type}", s.authHandler(s.resetHandler)).Methods(http.MethodPost)
	// list all users
	router.HandleFunc("/users", s.authHandler(s.viewUsersHandler)).Methods(http.MethodGet)
	// single user
	router.HandleFunc("/user", s.authHandler(s.manageUserHandler)).Methods(http.MethodPost)
	router.HandleFunc("/user/{username}", s.authHandler(s.manageUserHandler)).Methods(http.MethodGet, http.MethodPost)
	router.HandleFunc("/admin", s.authHandler(s.adminHandler)).Methods(http.MethodGet)
	router.HandleFunc("/admin/{type}", s.authHandler(s.adminHandler)).Methods(http.MethodPost)
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
		Addr:         net.JoinHostPort(s.host, fmt.Sprint(s.port)),
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
		authorised := s.userDB.AuthenticateUser(r)
		// if not logged in
		if authorised == false {
			// permitted routes for unauthenticated users
			if r.URL.String() == "/login" || strings.HasPrefix(r.URL.String(), "/reset") {
				h(w, r)
				return
			}

			// forbidden routes for unauthenticated users (redirect to login page)
			if r.Method == http.MethodGet {
				http.Redirect(w, r, "/login", http.StatusFound)
			} else {
				s.RespondStatus(w, r, "unauthorised", http.StatusUnauthorized)
			}
			return
		}

		// get logged in session user
		sessionUser, err := s.userDB.GetSessionUser(r)
		if err != nil {
			// if the user DB has been reset, this can invalidate sessions - users with invalid sessions will be logged
			// out automatically
			Input.Log(errors.Wrap(err, "could not fetch corresponding user session (maybe user DB was recently reset)"))
			s.logoutHandler(w, r)
			return
		}

		// redirect blocked users
		if sessionUser.AccountState == Blocked {
			s.RespondStatus(w, r, "user has been blocked", http.StatusUnauthorized)
			return
		}

		// new user has not created a password or user has logged in with reset temp password
		if sessionUser.PasswordResetRequired {
			// permit logging out when creating password after login
			if r.URL.String() == "/logout" {
				h(w, r)
				return
			}

			s.createNewPasswordHandler(w, r)
			return
		}

		// prevent login/reset page access when logged in
		if r.URL.String() == "/login" || strings.HasPrefix(r.URL.String(), "/reset") {
			if r.Method == http.MethodGet {
				http.Redirect(w, r, "/", http.StatusFound)
			} else {
				s.Respond(w, r, "already logged in")
			}
			return
		}

		// continue to call handler
		h(w, r)
	}
}

// fileServerAuthHandler is a file server authentication wrapper.
func (s *Server) fileServerAuthHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// prevent dir listings
		if r.URL.String() != "/" && strings.HasSuffix(r.URL.String(), "/") {
			s.RespondStatus(w, r, "404 page not found", http.StatusNotFound)
			return
		}

		// authenticate
		authorised := s.userDB.AuthenticateUser(r)
		// if not logged in
		if authorised {
			// whitelist
			sessionUser, err := s.userDB.GetSessionUser(r)
			if err != nil {
				Input.Log(errors.Wrap(err, "could not fetch corresponding user session"))
				return
			}

			// prevent blocked users
			if sessionUser.AccountState == Blocked {
				s.RespondStatus(w, r, "unauthorised", http.StatusUnauthorized)
				return
			}

			// prevent unauthorised access to temp uploaded files
			if strings.HasPrefix(r.URL.String(), "/temp_uploaded/") {
				vars := mux.Vars(r)

				if sessionUser.Username != vars["user_id"] {
					s.RespondStatus(w, r, "404 page not found", http.StatusNotFound)
					return
				}
			}
		} else {
			// blacklist
			if strings.HasPrefix(r.URL.String(), "/static/content/") {
				s.RespondStatus(w, r, "unauthorised", http.StatusUnauthorized)
				return
			}
		}

		h.ServeHTTP(w, r)
	})
}

// ServerError is an error type which contains a sensitive error message and a user friendly error message which can
// be safely returned to the user client.
type ServerError struct {
	// err is the error message which may be logged & may contain sensitive server error information
	err error
	// response is the response message which can be safely returned to the user client
	response string
}

// Error returns the potentially sensitive error message.
func (e *ServerError) Error() string {
	return e.err.Error()
}

// ResponseStatus represents an operation success state and is used by the UI to indicate the result of an operation.
type ResponseStatus string

var (
	// SuccessStatus represents a successful operation.
	SuccessStatus ResponseStatus = "success"
	// WarningStatus represents a failed operation due to invalid/forbidden user input.
	WarningStatus ResponseStatus = "warning"
	// ErrorStatus represents an internal server error.
	ErrorStatus ResponseStatus = "error"
)

// JSONResponse is used to return whether the operation was a success along with a value. When passed to respond(), it
// is automatically parsed into a JSON string.
type JSONResponse struct {
	Status ResponseStatus `json:"status"`
	Value  string         `json:"value"`
}

// resetHandler is a HTTP handler which manages user password reset requests and password setting requests.
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
		templateData.ContentHTML = s.CompleteTemplate("/dynamic/templates/forgotten_password.html", templateData)
		result := s.CompleteTemplate("/dynamic/templates/main.html", templateData)

		s.Respond(w, r, result)

	// submit password reset request
	case http.MethodPost:
		if s.ParseFormBody(w, r) != nil {
			return
		}

		vars := mux.Vars(r)

		switch vars["type"] {
		// request new password reset email
		case "request":
			recipientEmail := r.FormValue("email")

			// perform password reset & email sending in the background
			go s.sendPasswordResetEmail(recipientEmail)

		// set new password
		case "set":
			s.createNewPasswordHandler(w, r)
			return

		default:
			s.RespondStatus(w, r, "unsupported request", http.StatusBadRequest)
			return
		}

		s.Respond(w, r, "success")
		return
	}
}

// sendPasswordResetEmail sends an email with a temp password for account recovery & registration.
func (s *Server) sendPasswordResetEmail(recipientEmail string) {
	// set temp password if user exists (don't inform user of failed reset attempt to prevent address brute forcing)
	tempPass, err := s.userDB.SetTempPassword(recipientEmail)
	if err != nil {
		return
	}

	// TODO: delete me (exposes temp passwords to terminal)
	Info.Log("New password: ", tempPass)

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
		Critical.Log(errors.Wrap(err, "failed to reset email"))
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
		switch {
		case err != nil:
			Input.Log(err)
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
		Input.Log(errors.Wrap(err, "error logging out"))
		s.Respond(w, r, "error logging out")
		return
	}
	http.Redirect(w, r, "/login", http.StatusFound)
}

// createNewPasswordHandler is a HTTP handler which forces a user password reset upon login.
func (s *Server) createNewPasswordHandler(w http.ResponseWriter, r *http.Request) {
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
			"Create Password",
			config.ServiceName,
			sessionUser,
			"",
			"",
			"",
			"",
		}

		templateData.NavbarHTML = s.CompleteTemplate("/dynamic/templates/navbar.html", templateData)
		templateData.FooterHTML = s.CompleteTemplate("/dynamic/templates/footers/login_footer.html", templateData)
		templateData.ContentHTML = s.CompleteTemplate("/dynamic/templates/create_password.html", templateData)
		result := s.CompleteTemplate("/dynamic/templates/main.html", templateData)

		s.Respond(w, r, result)

	case http.MethodPost:
		// form is already parsed in calling func
		password := r.FormValue("password")
		passwordConfirm := r.FormValue("confirm-password")

		// validate password
		if err := s.userDB.ValidatePassword(password); err != nil {
			s.Respond(w, r, JSONResponse{WarningStatus, err.response})
			return
		}

		// passwords don't match
		if password != passwordConfirm {
			s.Respond(w, r, JSONResponse{WarningStatus, "invalid_password_matching"})
			return
		}

		// attempt to set password
		if err := s.userDB.SetNewUserPassword(sessionUser.Username, password); err != nil {
			Input.Log(err.Error())
			s.Respond(w, r, JSONResponse{WarningStatus, err.response})
			return
		}

		s.Respond(w, r, JSONResponse{SuccessStatus, "success"})
	}
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
			if s.ParseFormBody(w, r) != nil {
				return
			}

			// check auth or admin privs first
			s.Respond(w, r, "not yet implemented")
		}
		return
	}

	switch r.Method {
	// add a file to a user's favourites -> /user
	case http.MethodPost:
		if s.ParseFormBody(w, r) != nil {
			return
		}

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
}

// represents a user creation request
type UserCreationDetails struct {
	Forename    string `json:"forename"`
	Surname     string `json:"surname"`
	AccountType int    `json:"account-type,string"`
	Email       string `json:"email"`
}

// adminHandler is a HTTP handler which manages all admin related tasks such as user creation & management, service
// settings & statistics.
func (s *Server) adminHandler(w http.ResponseWriter, r *http.Request) {
	// get session user
	sessionUser, err := s.userDB.GetSessionUser(r)
	if err != nil {
		Critical.Log(err)
		s.Respond(w, r, "error")
		return
	}

	// check user is admin or super admin
	if sessionUser.Type < Admin {
		Input.Log(fmt.Sprintf("user %v does not have admin privileges", sessionUser.Username))
		http.Redirect(w, r, "/", http.StatusFound)
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
			"Admin",
			config.ServiceName,
			sessionUser,
			"",
			"admin",
			"",
			"",
		}

		templateData.NavbarHTML = s.CompleteTemplate("/dynamic/templates/navbar.html", templateData)
		templateData.FooterHTML = s.CompleteTemplate("/dynamic/templates/footers/admin_footer.html", templateData)
		templateData.ContentHTML = s.CompleteTemplate("/dynamic/templates/admin.html", templateData)
		result := s.CompleteTemplate("/dynamic/templates/main.html", templateData)

		s.Respond(w, r, result)
		return

	case http.MethodPost:
		vars := mux.Vars(r)

		switch vars["type"] {
		case "createuser":
			// read JSON user creation details from body
			var details UserCreationDetails
			if err := json.NewDecoder(r.Body).Decode(&details); err != nil {
				Input.Log(errors.Wrap(err, "failed to parse createuser body to JSON"))
				s.Respond(w, r, JSONResponse{WarningStatus, "invalid request"})
				return
			}

			// prevent creating accounts of higher permissions than the session user
			if UserType(details.AccountType) > sessionUser.Type && sessionUser.Type <= SuperAdmin {
				Input.Log("insufficient permissions")
				s.Respond(w, r, JSONResponse{WarningStatus, "insufficient_permissions"})
				return
			}

			// create new user
			user, err := s.userDB.AddUser(details.Forename, details.Surname, details.Email, UserType(details.AccountType))
			if err != nil {
				Input.Log(errors.Wrap(err, "failed to create new user"))
				s.Respond(w, r, JSONResponse{WarningStatus, err.response})
				return
			}

			// email temp password to new user
			go s.sendPasswordResetEmail(user.Email)

			s.Respond(w, r, JSONResponse{SuccessStatus, user.Username})

		case "manageusers":
			s.Respond(w, r, "ok")

		case "requests":
			s.Respond(w, r, "ok")

		case "settings":
			s.Respond(w, r, "ok")

		case "stats":
			s.Respond(w, r, "ok")

		default:
			Input.Log("invalid request type")
			s.Respond(w, r, "invalid request type")
		}

	}
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
		if s.ParseFormBody(w, r) != nil {
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
		// prevent guests from uploading
		if sessionUser.Type == Guest {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}

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
		if sessionUser.Type == Guest {
			s.RespondStatus(w, r, "unauthorised", http.StatusUnauthorized)
			return
		}

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

			// move from form file to temp dir & create FileDB Uploaded entry
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

			// increment uploads count for user
			sessionUser.UploadsCount++
			s.userDB.Users.Set(sessionUser.Username, sessionUser)

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
			if s.ParseFormBody(w, r) != nil {
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
			if s.ParseFormBody(w, r) != nil {
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

			// increment published count for user
			sessionUser.PublishedCount++
			s.userDB.Users.Set(sessionUser.Username, sessionUser)

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
	case JSONResponse:
		json, err := json.Marshal(response)
		if err != nil {
			Output.Log("failed to parse JSON response: ", err)
		}
		response = string(json)
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

// ParseFormBody parses a request's form based body.
func (s *Server) ParseFormBody(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		Input.Log(errors.Wrap(err, "error parsing body form"))
		s.Respond(w, r, "error")
		return err
	}
	return nil
}
