package memoryshare

import (
	"encoding/gob"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
)

// The operation a transaction performed.
type AccountState int

const (
	UNREGISTERED    AccountState = iota // waiting for both admin confirmation & user email confirmation
	ADMIN_CONFIRMED                     // waiting for email confirmation
	EMAIL_CONFIRMED                     // waiting for admin to confirm user
	COMPLETE
	BLOCKED
)

// Determines the permissions owned by a user.
type UserType int

const (
	STANDARD    UserType = iota
	ADMIN                // can add/block users, can make others admin
	SUPER_ADMIN          // cannot be removed, can change user details (such as admin privs, but not on self), can complete file edit/delete requests
	GUEST                // can view/search files only, cannot upload
)

// A user account.
type User struct {
	Username           string // generally found in URLs
	Email              string
	Password           string
	TempResetPassword  string
	Forename           string
	Surname            string
	Type               UserType
	CreatedTimestamp   int64
	Image              string
	FavouriteFileUUIDs map[string]bool // fileUUID key
	AccountState
}

// The DB where files are stored.
type UserDB struct {
	// username key, User object value
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
	go userDB.startAccessPoller()

	// create default super admin account if no users exist
	if len(userDB.Users) == 0 {
		Info.Log("> Create the default super admin account.")

		for {
			userAccReq := UserAccessRequest{operation: "addUser", attributes: make(map[string]string), userType: SUPER_ADMIN}

			// get forename, surname, email, password
			if userAccReq.attributes["forename"], err = ReadStdin("> Forename: \n", false); err != nil {
				Critical.Log("> Error reading console input...")
				continue
			}
			if userAccReq.attributes["surname"], err = ReadStdin("> Surname: \n", false); err != nil {
				Critical.Log("> Error reading console input...")
				continue
			}
			if userAccReq.attributes["email"], err = ReadStdin("> Email: \n", false); err != nil {
				Critical.Log("> Error reading console input...")
				continue
			}
			if userAccReq.attributes["password"], err = ReadStdin("> Password: \n", true); err != nil {
				Critical.Log("> Error reading console input...")
				continue
			}

			// perform account creation request
			response := userDB.performAccessRequest(userAccReq)
			if response.err == nil {
				// set state to ok
				user := userDB.Users[response.username]
				user.AccountState = COMPLETE
				userDB.Users[response.username] = user
				break
			}

			if response.err.Error() == "error" {
				response.err = fmt.Errorf("internal error")
			} else {
				response.err = fmt.Errorf(strings.Replace(response.err.Error(), "_", " ", -1))
			}

			Info.Logf("> Account creation failed: %s. Try again to create the default super admin account.\n\n", response.err.Error())
		}
	}

	return
}

// Structure for passing request and response data between poller.
type UserAccessRequest struct {
	attributes     map[string]string
	userType       UserType
	w              http.ResponseWriter
	r              *http.Request
	operation      string
	userIdentifier string
	fileUUID       string
	state          bool
	response       chan UserAccessResponse
}
type UserAccessResponse struct {
	err      error
	success  bool
	username string
	user     User
	users    []User
}

// Create a blocking access request and provide an access response.
func (db *UserDB) performAccessRequest(request UserAccessRequest) (response UserAccessResponse) {
	request.response = make(chan UserAccessResponse, 1)
	db.requestPool <- request
	return <-request.response
}

// Poll for requests, process them & pass result/error back to requester via channels.
func (db *UserDB) startAccessPoller() {
	for req := range db.requestPool {
		response := UserAccessResponse{}

		// process request
		switch req.operation {
		case "addUser":
			if len(req.attributes) < 4 {
				response.err = fmt.Errorf("email or password not specified")
			} else {
				response.username, response.err = db.addUser(req.attributes["email"], req.attributes["password"], req.attributes["forename"], req.attributes["surname"], req.userType)
				db.serializeToFile()
			}

		case "authenticateUser":
			response.success, response.err = db.authenticateUser(req.w, req.r)

		case "getSessionUser":
			response.user, response.err = db.getSessionUser(req.w, req.r)

		case "getUsers":
			response.users = db.getUsers()

		case "getUserByEmail":
			response.user, response.err = db.getUserByEmail(req.userIdentifier)

		case "getUserByUsername":
			response.user, response.err = db.getUserByUsername(req.userIdentifier)

		case "setFavourite":
			response.err = db.setFavourite(req.userIdentifier, req.fileUUID, req.state)
			db.serializeToFile()

		case "loginUser":
			response.success, response.err = db.loginUser(req.w, req.r)
			db.serializeToFile()

		case "logoutUser":
			response.err = db.logoutUser(req.w, req.r)
			db.serializeToFile()

		default:
			response.err = fmt.Errorf("unsupported user access operation")
		}

		req.response <- response
	}
}

// Add a user to userDB.
func (db *UserDB) addUser(email string, password string, forename string, surname string, admin UserType) (username string, err error) {
	// validate inputs
	var nameRegex = regexp.MustCompile(`^[A-Za-z ,.'-]+$`).MatchString
	var emailRegex = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`).MatchString

	if len(forename) == 0 || !nameRegex(forename) {
		return "", fmt.Errorf("invalid_forename")
	}
	if len(surname) == 0 || !nameRegex(surname) {
		return "", fmt.Errorf("invalid_surname")
	}
	if !emailRegex(email) {
		return "", fmt.Errorf("invalid_email")
	}

	// minimum eight characters, at least one upper case letter, one number and one special character
	containsUpper := false
	containsNumber := false
	containsSpecial := false
	for _, c := range password {
		switch {
		case unicode.IsNumber(c):
			containsNumber = true
		case unicode.IsUpper(c):
			containsUpper = true
		case unicode.IsPunct(c) || unicode.IsSymbol(c):
			containsSpecial = true
		}
	}

	if !containsUpper || !containsNumber || !containsSpecial || len(password) < 8 {
		return "", fmt.Errorf("invalid_password")
	}

	// check if user exists already
	if _, err := db.getUserByEmail(email); err == nil {
		return "", fmt.Errorf("account_already_exists")
	}

	// hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	if err != nil {
		Critical.Log("error hashing password")
		return "", fmt.Errorf("error")
	}

	newUser := User{Password: string(hashedPassword), Email: email, Type: admin, Forename: forename, Surname: surname, CreatedTimestamp: time.Now().UnixNano(), FavouriteFileUUIDs: make(map[string]bool)}

	// create username, appending an incremented number if a user exists with that username already (ensures value is unique)
	usernameCounter := 1
	for {
		newUser.Username = newUser.Forename + newUser.Surname
		if usernameCounter > 1 {
			newUser.Username += fmt.Sprintf("%d", usernameCounter)
		}
		exists := false
		for email := range db.Users {
			if _, ok := db.Users[email]; ok {
				exists = true
				break
			}
		}
		if !exists {
			break
		}
		usernameCounter++
	}

	// add user to DB
	db.Users[newUser.Username] = newUser
	Creation.Log("new user created: " + newUser.Username)
	return newUser.Username, nil
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
func (db *UserDB) getSessionUser(w http.ResponseWriter, r *http.Request) (user User, err error) {
	session, err := db.cookies.Get(r, "memory-share")
	if err != nil {
		return
	}

	user, err = db.getUserByEmail(session.Values["email"].(string))
	return user, err
}

// Add a file UUID to the favourites list of a user.
func (db *UserDB) setFavourite(username string, fileUUID string, state bool) (err error) {
	user, ok := db.Users[username]
	if !ok {
		return fmt.Errorf("user_not_found")
	}

	favourites := user.FavouriteFileUUIDs
	favourites[fileUUID] = state

	if state == false {
		delete(favourites, fileUUID)
	}

	user.FavouriteFileUUIDs = favourites
	db.Users[username] = user
	return
}

// Get a copy of the users map.
func (db *UserDB) getUsers() (users []User) {
	for username := range db.Users {
		users = append(users, db.Users[username])
	}

	// order by date created
	sort.Slice(users, func(i, j int) bool {
		return users[i].CreatedTimestamp > users[j].CreatedTimestamp
	})
	return
}

// Get user that ha sa target email address.
func (db *UserDB) getUserByEmail(email string) (user User, err error) {
	for _, u := range db.Users {
		if u.Email == email {
			user = u
			break
		}
	}
	if user.Email == "" {
		err = fmt.Errorf("user not found")
	}
	return
}

// Get user that ha sa target username.
func (db *UserDB) getUserByUsername(username string) (user User, err error) {
	user, ok := db.Users[username]
	if !ok {
		err = fmt.Errorf("user not found")
	}
	return
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
	for _, user := range db.Users {
		// compare stored hash against hash of input password
		err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(passwordParam))
		if emailParam == user.Email && err == nil {
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
	sessionFilePath := config.RootPath + "/config/session_key.dat"

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
