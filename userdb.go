package main

import (
	"encoding/gob"
	"fmt"
	"net/http"
	"os"

	"golang.org/x/crypto/bcrypt"

	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
)

// The operation a transaction performed.
type AccountState int

const (
	REGISTER_EMAIL AccountState = iota // waiting for email confirmation
	REGISTER_CONFIRM
	COMPLETE // waiting for admin to confirm user
	BLOCKED
)

// A user account.
type User struct {
	UUID     string
	password string
	admin    bool
	AccountState
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
	// get session key
	key, err := fetchSessionKey()
	if err != nil {
		return nil, err
	}

	var cookieStore = sessions.NewCookieStore(key)
	userDB = &UserDB{cookies: cookieStore, dir: dbDir, file: dbDir + "/user_db.dat", Users: make(map[string]User)}

	// load users & sessions from file

	// create default admin account if no users exist
	if len(userDB.Users) == 0 {
		userDB.addUser("admin", "admin", true)
	}

	// start request poller
	go userDB.StartUserAccessPoller()

	return
}

// Structure for passing request and response data between poller.
type UserAccessRequest struct {
	stringsIn []string
	boolIn    bool
	writerIn  http.ResponseWriter
	reqIn     *http.Request
	operation string
	response  chan UserAccessResponse
}
type UserAccessResponse struct {
	err     error
	success bool
}

// Poll for requests, process them & pass result/error back to requester via channels.
func (db *UserDB) StartUserAccessPoller() {
	db.requestPool = make(chan UserAccessRequest)

	for req := range db.requestPool {
		response := UserAccessResponse{}

		// process request
		switch req.operation {
		case "addUser":
			if len(req.stringsIn) < 2 {
				response.err = fmt.Errorf("email or password not specified")
			} else {
				response.err = db.addUser(req.stringsIn[0], req.stringsIn[1], req.boolIn)
			}

		case "authenticateUser":
			response.success, response.err = db.authenticateUser(req.writerIn, req.reqIn)

		case "loginUser":
			response.success, response.err = db.loginUser(req.writerIn, req.reqIn)

		case "logoutUser":
			response.err = db.logoutUser(req.writerIn, req.reqIn)

		default:
			response.err = fmt.Errorf("unsupported user access operation")
		}

		req.response <- response
	}
}

// Add a user to userDB.
func (db *UserDB) addUser(email string, password string, admin bool) (err error) {
	// check if user exists already
	if _, ok := db.Users[email]; ok {
		return fmt.Errorf("an account already exists with this email address")
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	if err != nil {
		return err
	}

	newUser := User{password: string(hashedPassword), admin: admin, UUID: NewUUID()}
	db.Users[email] = newUser
	return
}

// Authenticate user.
func (db *UserDB) authenticateUser(w http.ResponseWriter, r *http.Request) (success bool, err error) {
	session, err := db.cookies.Get(r, "memory-share")
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
func (db *UserDB) loginUser(w http.ResponseWriter, r *http.Request) (success bool, err error) {
	session, err := db.cookies.Get(r, "memory-share")
	if err != nil {
		return false, err
	}

	// get form data
	if err = r.ParseForm(); err != nil {
		return false, err
	}
	emailParam := r.FormValue("email")
	passwordParam := r.FormValue("password")

	// check form data against user DB
	matchFound := false
	for email, user := range db.Users {
		// compare stored hash against hash of input password
		err := bcrypt.CompareHashAndPassword([]byte(user.password), []byte(passwordParam))
		if emailParam == email && err == nil {
			matchFound = true
			break
		}
	}
	if matchFound == false {
		return false, nil
	}

	// set user as authenticated
	session.Values["authenticated"] = true
	session.Values["email"] = emailParam
	// session expires after 7 days
	session.Options = &sessions.Options{
		Path:   "/",
		MaxAge: 86400 * 7,
	}
	if err := session.Save(r, w); err != nil {
		return false, err
	}

	return true, nil
}

// Perform user logout.
func (db *UserDB) logoutUser(w http.ResponseWriter, r *http.Request) (err error) {
	session, err := db.cookies.Get(r, "memory-share")
	if err != nil {
		return err
	}

	// Revoke users authentication
	session.Values["authenticated"] = false
	if err := session.Save(r, w); err != nil {
		return err
	}
	return nil
}

// Get session secure key from session.dat if one was created in the previous run, otherwise create a new one
func fetchSessionKey() (key []byte, err error) {
	sessionFilePath := config.rootPath + "/config/session.dat"

	// check if file exists
	ok, err := FileOrDirExists(sessionFilePath)
	if err != nil {
		return nil, err
	}
	if !ok {
		// create file for writing to
		file, err := os.Create(sessionFilePath)
		if err != nil {
			return nil, err
		}
		defer file.Close()

		key := securecookie.GenerateRandomKey(64)
		if key == nil {
			return nil, fmt.Errorf("could not generate session key")
		}

		// encode to file
		encoder := gob.NewEncoder(file)
		err = encoder.Encode(&key)
		if err != nil {
			return nil, err
		}

		return key, nil
	}

	// open file to read from
	file, err := os.Open(sessionFilePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// decode file contents to store map
	decoder := gob.NewDecoder(file)
	if err = decoder.Decode(&key); err != nil {
		return nil, err
	}

	return key, nil
}
