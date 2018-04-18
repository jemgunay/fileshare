package memoryshare

import (
	"encoding/gob"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
)

// AccountState represents the registration state of a User account.
type AccountState int

const (
	// UNREGISTERED represents an account waiting for both admin confirmation & user email confirmation.
	UNREGISTERED AccountState = iota
	// ADMIN_CONFIRMED represents an account waiting for user email confirmation only.
	ADMIN_CONFIRMED
	// EMAIL_CONFIRMED represents an account waiting for admin confirmation only.
	EMAIL_CONFIRMED
	// COMPLETE represents an account which has completed the registration process.
	COMPLETE
	// BLOCKED represents an account which has been blocked from logging in.
	BLOCKED
)

// UserType represents the permissions owned by a user.
type UserType int

const (
	// STANDARD accounts can perform standard actions.
	STANDARD    UserType = iota
	// ADMIN accounts can add/block users and can make others admin.
	ADMIN
	// SUPER_ADMIN accounts cannot be removed, can change user details (such as admin privs, but not on self) and can complete file edit/delete requests.
	SUPER_ADMIN
	// GUEST accounts can view/search files only and cannot upload.
	GUEST
)

// User represents a user account.
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

// UserMapMutex wraps all Users to permit safe concurrent access.
type UserMapMutex struct {
	Users map[string]User
	mu    sync.Mutex
}

// Set creates or updates a User in a UserDB.
func (fm *UserMapMutex) Set(username string, user User) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.Users[username] = user
}

// Get attempts to retrieve a User from a UserDB.
func (fm *UserMapMutex) Get(username string) (user User, ok bool) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	user, ok = fm.Users[username]
	return
}

// Count returns the number of Users in a UserDB.
func (fm *UserMapMutex) Count() (size int) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	return len(fm.Users)
}

// Delete removes a User from a UserDB.
func (fm *UserMapMutex) Delete(username string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	delete(fm.Users, username)
}

// UserMapDB is a User container, where the map key is the User's username.
type UserMapDB map[string]User

// UserMapFunc is used to pass functions to PerformFunc which allows concurrency safe UserDB access.
type UserMapFunc func(UserMapDB) interface{}

// PerformFunc executes the UserMapFunc, wrapping it in a Mutex lock to serialise access. This is used for more complex
// operations where many locking and unlocking operations would have been required otherwise.
func (fm *UserMapMutex) PerformFunc(userMapFunc UserMapFunc) interface{} {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	return userMapFunc(fm.Users)
}

// The DB where files are stored.
type UserDB struct {
	// username key, User object value
	Users   UserMapMutex
	cookies *sessions.CookieStore
	dir     string
	file    string
}

// Create a new user DB.
func NewUserDB(dbDir string) (userDB *UserDB, err error) {
	// get session key
	key, err := fetchSessionKey()
	if err != nil {
		return nil, err
	}

	userDB = &UserDB{
		cookies: sessions.NewCookieStore(key),
		dir:     dbDir,
		file:    dbDir + "/user_db.dat",
		Users:   UserMapMutex{Users: make(map[string]User)},
	}

	// load users
	userDB.deserializeFromFile()

	// create default super admin account if no users exist
	if userDB.Users.Count() == 0 {
		Info.Log("> Create the default super admin account.")
		userDB.createActivatedUser(SUPER_ADMIN)
	}

	return
}

func (db *UserDB) createActivatedUser(accountType UserType) {
	var forename, surname, email, password string
	var err error

	for {
		// get forename, surname, email, password
		if forename, err = ReadStdin("> Forename: \n", false); err != nil {
			Critical.Log("> Error reading console input...")
			continue
		}
		if surname, err = ReadStdin("> Surname: \n", false); err != nil {
			Critical.Log("> Error reading console input...")
			continue
		}
		if email, err = ReadStdin("> Email: \n", false); err != nil {
			Critical.Log("> Error reading console input...")
			continue
		}
		if password, err = ReadStdin("> Password: \n", true); err != nil {
			Critical.Log("> Error reading console input...")
			continue
		}

		// perform account creation request
		username, err := db.addUser(forename, surname, email, password, accountType)
		if err != nil {
			if err.Error() == "error" {
				err = fmt.Errorf("internal error")
			} else {
				err = fmt.Errorf(strings.Replace(err.Error(), "_", " ", -1))
			}

			Critical.Logf("> Account creation failed: %s. Try again to create the default super admin account.\n\n", err)
			continue
		}

		// set state to ok
		user, ok := db.Users.Get(username)
		if !ok {
			Critical.Logf("> Account creation failed: %s. Try again to create the default super admin account.\n\n", "created user not found")
			continue
		}
		user.AccountState = COMPLETE
		db.Users.Set(username, user)
		return
	}
}

// Add a user to userDB.
func (db *UserDB) addUser(forename string, surname string, email string, password string, admin UserType) (username string, err error) {
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

	newUser := User{
		Password:           string(hashedPassword),
		Email:              email,
		Type:               admin,
		Forename:           forename,
		Surname:            surname,
		CreatedTimestamp:   time.Now().UnixNano(),
		FavouriteFileUUIDs: make(map[string]bool),
	}

	// create username, appending an incremented number if a user exists with that username already (ensures value is unique)
	usernameCounter := 1
	for {
		newUser.Username = newUser.Forename + newUser.Surname
		if usernameCounter > 1 {
			newUser.Username += fmt.Sprintf("%d", usernameCounter)
		}

		// username has not been taken
		if _, ok := db.Users.Get(username); !ok {
			break
		}

		// username was taken, increment counter and try again
		usernameCounter++
	}

	// add user to DB
	db.Users.Set(newUser.Username, newUser)
	db.serializeToFile()

	Creation.Log("new user created: " + newUser.Username)
	return newUser.Username, nil
}

// Authenticate user.
func (db *UserDB) authenticateUser(r *http.Request) (success bool, err error) {
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
func (db *UserDB) getSessionUser(r *http.Request) (user User, err error) {
	session, err := db.cookies.Get(r, "memory-share")
	if err != nil {
		return
	}

	user, err = db.getUserByEmail(session.Values["email"].(string))
	return user, err
}

// Add a file UUID to the favourites list of a user.
func (db *UserDB) setFavourite(username string, fileUUID string, state bool) (err error) {
	user, ok := db.Users.Get(username)
	if !ok {
		return fmt.Errorf("user_not_found")
	}

	favourites := user.FavouriteFileUUIDs
	favourites[fileUUID] = state

	if state == false {
		delete(favourites, fileUUID)
	}

	user.FavouriteFileUUIDs = favourites
	db.Users.Set(username, user)
	db.serializeToFile()
	return
}

// Get a slice copy of all each User from the Users map.
func (db *UserDB) getUsers() []User {
	getAllUsers := func(m UserMapDB) interface{} {
		var users []User
		for _, user := range m {
			users = append(users, user)
		}
		return users
	}
	users := db.Users.PerformFunc(getAllUsers).([]User)

	// order by date created
	sort.Slice(users, func(i, j int) bool {
		return users[i].CreatedTimestamp > users[j].CreatedTimestamp
	})
	return users
}

// Get user that has a target email address.
func (db *UserDB) getUserByEmail(email string) (User, error) {
	userSearch := func(m UserMapDB) interface{} {
		for _, u := range m {
			if u.Email == email {
				return u
			}
		}
		return User{}
	}

	user := db.Users.PerformFunc(userSearch).(User)

	if user.Email == "" {
		return user, fmt.Errorf("user not found")
	}
	return user, nil
}

// Get user that ha sa target username.
func (db *UserDB) getUserByUsername(username string) (user User, err error) {
	user, ok := db.Users.Get(username)
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
	hashCompare := func(m UserMapDB) interface{} {
		for _, user := range m {
			// compare stored hash against hash of input password
			err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(passwordParam))
			if emailParam == user.Email && err == nil {
				return true
			}
		}
		return false
	}

	if db.Users.PerformFunc(hashCompare).(bool) == false {
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
	db.Users.mu.Lock()
	defer db.Users.mu.Unlock()

	// create/truncate file for writing to
	file, err := os.Create(db.file)
	if err != nil {
		Critical.Log(err)
		return err
	}
	defer file.Close()

	// encode store map to file
	encoder := gob.NewEncoder(file)
	err = encoder.Encode(&db)
	if err != nil {
		Critical.Log(err)
		return err
	}

	return nil
}

// Deserialize from a specified file to the store map, overwriting current map values.
func (db *UserDB) deserializeFromFile() (err error) {
	db.Users.mu.Lock()

	// if db file does not exist, create a new one
	if _, err := os.Stat(db.file); os.IsNotExist(err) {
		db.Users.mu.Unlock()
		db.serializeToFile()
		return nil
	}
	defer db.Users.mu.Unlock()

	// open file to read from
	file, err := os.Open(db.file)
	if err != nil {
		Critical.Log(err)
		return err
	}
	defer file.Close()

	// decode file contents to store map
	decoder := gob.NewDecoder(file)
	if err = decoder.Decode(&db); err != nil {
		Critical.Log(err)
		return err
	}

	return nil
}
