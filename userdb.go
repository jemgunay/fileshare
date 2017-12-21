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
	Password string
	Admin    bool
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
	userDB = &UserDB{cookies: cookieStore, dir: dbDir, file: dbDir + "/user_db.dat", Users: make(map[string]User), requestPool: make(chan UserAccessRequest)}

	// load users
	userDB.deserializeFromFile()

	// start request poller
	go userDB.StartUserAccessPoller()

	// create default admin account if no users exist
	if len(userDB.Users) == 0 {
		userAR := UserAccessRequest{response: make(chan UserAccessResponse), operation: "addUser", stringsIn: []string{"admin", "admin"}, boolIn: true}
		userDB.requestPool <- userAR
		if (<-userAR.response).err != nil {
			return nil, fmt.Errorf("default admin account could not be created")
		}
	}

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
	user    *User
}

// Poll for requests, process them & pass result/error back to requester via channels.
func (db *UserDB) StartUserAccessPoller() {
	for req := range db.requestPool {
		response := UserAccessResponse{}

		// process request
		switch req.operation {
		case "addUser":
			if len(req.stringsIn) < 2 {
				response.err = fmt.Errorf("email or password not specified")
			} else {
				response.err = db.addUser(req.stringsIn[0], req.stringsIn[1], req.boolIn)
				db.serializeToFile()
			}

		case "authenticateUser":
			response.success, response.err = db.authenticateUser(req.writerIn, req.reqIn)

		case "getSessionUser":
			response.user, response.err = db.getSessionUser(req.writerIn, req.reqIn)

		case "loginUser":
			response.success, response.err = db.loginUser(req.writerIn, req.reqIn)
			db.serializeToFile()

		case "logoutUser":
			response.err = db.logoutUser(req.writerIn, req.reqIn)
			db.serializeToFile()

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

	newUser := User{Password: string(hashedPassword), Admin: admin, UUID: NewUUID()}
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

// Get User object associated with request session.
func (db *UserDB) getSessionUser(w http.ResponseWriter, r *http.Request) (user *User, err error) {
	session, err := db.cookies.Get(r, "memory-share")
	if err != nil {
		return nil, err
	}

	sessionUser := db.Users[session.Values["email"].(string)]
	return &sessionUser, nil
}

// Perform user login.
func (db *UserDB) loginUser(w http.ResponseWriter, r *http.Request) (success bool, err error) {
	session, err := db.cookies.Get(r, "memory-share")

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
		err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(passwordParam))
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
	session.Options.MaxAge = -1
	if err := session.Save(r, w); err != nil {
		return err
	}
	return nil
}

// Get session secure key from session_key.dat if one was created in the previous run, otherwise create a new one.
func fetchSessionKey() (key []byte, err error) {
	sessionFilePath := config.rootPath + "/config/session_key.dat"

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

// Serialize store map & transactions slice to a specified file.
func (db *UserDB) serializeToFile() (err error) {
	// create/truncate file for writing to
	file, err := os.Create(db.file)
	if err != nil {
		return err
	}
	defer file.Close()

	// encode store map to file
	encoder := gob.NewEncoder(file)
	err = encoder.Encode(&db)
	if err != nil {
		return err
	}

	return nil
}

// Deserialize from a specified file to the store map, overwriting current map values.
func (db *UserDB) deserializeFromFile() (err error) {
	// if db file does not exist, create a new one
	if _, err := os.Stat(db.file); os.IsNotExist(err) {
		db.serializeToFile()
		return nil
	}

	// open file to read from
	file, err := os.Open(db.file)
	if err != nil {
		return err
	}
	defer file.Close()

	// decode file contents to store map
	decoder := gob.NewDecoder(file)
	if err = decoder.Decode(&db); err != nil {
		return err
	}

	return nil
}
