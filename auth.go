package main

import (
	"net/http"

	"fmt"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"crypto/sha256"
)

// The operation a transaction performed.
type AccountState int

const (
	COMPLETE         AccountState = iota
	REGISTER_EMAIL                // waiting for email confirmation
	REGISTER_CONFIRM              // waiting for admin to confirm user
)

// A user account.
type User struct {
	UUID     string
	password string
	blocked  bool
}

// The DB where files are stored.
type UserDB struct {
	// email key, User object value
	Users       map[string]User
	cookies     *sessions.CookieStore
	dir         string
	file        string
	requestPool chan UserAccessRequest
}

// Create a new user DB.
func NewUserDB(dbDir string) (userDB *UserDB, err error) {
	var cookieStore = sessions.NewCookieStore(securecookie.GenerateRandomKey(64))
	userDB = &UserDB{cookies: cookieStore, dir: dbDir, file: dbDir + "/user_db.dat"}

	// start request poller
	go userDB.StartFileAccessPoller()

	return
}

// Authenticate user.
func (db *UserDB) Authenticate(w http.ResponseWriter, req *http.Request) (success bool, err error) {
	session, err := db.cookies.Get(req, "cookie-name")
	if err != nil {
		return false, err
	}

	// check if user is authenticated
	if auth, ok := session.Values["authenticated"].(bool); !ok || !auth {
		return false, nil
	}
	
	return true, nil
}

// Perform user login.
func (db *UserDB) Login(w http.ResponseWriter, req *http.Request) (success bool, err error) {
	session, err := db.cookies.Get(req, "cookie-name")
	if err != nil {
		return false, err
	}

	// get form data
	emailParam := req.Form.Get("email")
	passwordParam := db.HashPassword(req.Form.Get("password"))

	// check form data against user DB
	for email, user := range db.Users {
		if emailParam == email && passwordParam == user.password {
			// Set user as authenticated
			session.Values["authenticated"] = true
			session.Save(req, w)
			return true, nil
		} 
	}
	
	// Set user as authenticated
	session.Values["authenticated"] = false
	session.Save(req, w)
	return false, nil
}

// Perform user logout.
func (db *UserDB) Logout(w http.ResponseWriter, req *http.Request) (err error) {
	session, err := db.cookies.Get(req, "cookie-name")
	if err != nil {
		return err
	}

	// Revoke users authentication
	session.Values["authenticated"] = false
	session.Save(req, w)
	return nil
}

// Structure for passing request and response data between poller.
type UserAccessRequest struct {
	stringOut chan string
	stringIn  chan string
	errorOut  chan error
	operation string
}

// Poll for requests, process them & pass result/error back to requester via channels.
func (db *UserDB) StartFileAccessPoller() {
	db.requestPool = make(chan UserAccessRequest)

	for req := range db.requestPool {
		// process request
		switch req.operation {
		case "login":

		case "logout":

		default:
			req.errorOut <- fmt.Errorf("unsupported user access operation")
		}
	}
}

// Hash a password (sha256).
func (db *UserDB) HashPassword(password string) (hash string) {
	h := sha256.New()
	h.Write([]byte("hello world\n"))
	return string(h.Sum(nil))
}