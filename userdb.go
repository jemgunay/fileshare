package memoryshare

import (
	"encoding/gob"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"sync"
	"time"
	"unicode"

	"github.com/dchest/uniuri"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"github.com/pkg/errors"
	"golang.org/x/crypto/bcrypt"
)

// AccountState represents the registration state of a User account.
type AccountState int

const (
	// AwaitingConfirmation represents an account waiting for user email confirmation.
	AwaitingConfirmation AccountState = iota
	// Registered represents an account which has been confirmed by .
	Registered
	// Blocked represents an account which has been blocked from logging in.
	Blocked
)

// UserType represents the permissions owned by a user.
type UserType int

const (
	// Standard accounts can perform standard actions.
	Standard UserType = iota
	// Guest accounts can view/search files only and cannot upload.
	Guest
	// Admin accounts can add/block users and can change user privs and can complete file edit/delete requests.
	Admin
	// SuperAdmin accounts cannot be removed, can change user details (such as admin privs, but not on self).
	SuperAdmin
)

// User represents a user account.
type User struct {
	Username               string // unique, though generally only used for display (found in URLs/search)
	Email                  string // used for unique identification/logging in etc
	Password               string
	LoginCount             int
	LoginTimestamp         int64
	TempResetPassword      string
	PasswordResetTimestamp time.Time
	PasswordResetRequired  bool
	Forename               string
	Surname                string
	Type                   UserType
	CreatedTimestamp       int64
	Image                  string
	FavouriteFileUUIDs     map[string]bool // fileUUID key
	UploadsCount           int
	PublishedCount         int
	AccountState
}

// UserMapMutex wraps all Users to permit safe concurrent access. Map key is the username.
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

// UserDB is the database where Users, their sessions and Metadata are stored.
type UserDB struct {
	Users   UserMapMutex
	cookies *sessions.CookieStore
	dir     string
	file    string
}

// NewUserDB initialises the UserDB container and populates it with data from the stored file if possible. Otherwise,
// a new file is created containing the serialized empty UserDB. A default super admin account is also created
// via command line if no users are found in the DB.
func NewUserDB(dbDir string) (userDB *UserDB, err error) {
	// get session key
	key, err := FetchSessionKey()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch session key")
	}

	userDB = &UserDB{
		cookies: sessions.NewCookieStore(key),
		dir:     dbDir,
		file:    dbDir + "/user_db.dat",
		Users:   UserMapMutex{Users: make(map[string]User)},
	}

	// load DB from file
	if err = userDB.DeserializeFromFile(); err != nil {
		err = errors.Wrap(err, "could not deserialize UserDB from file")
		return
	}

	// create default super admin account if no users exist
	if userDB.Users.Count() == 0 {
		Info.Log("> Create the default super admin account.")
		userDB.CreateActivatedUser(SuperAdmin)
	}

	return
}

// CreateActivatedUser creates a new User and bypasses the email & admin verification.
func (db *UserDB) CreateActivatedUser(accountType UserType) {
	var forename, surname, email, password, retypePassword string
	var err error

	// get forename, surname, email
	for {
		inputDetailsErr := func() error {
			if forename, err = ReadStdin("> Forename:", false); err != nil {
				return err
			}
			if surname, err = ReadStdin("> Surname:", false); err != nil {
				return err
			}
			if email, err = ReadStdin("> Email:", false); err != nil {
				return err
			}
			return nil
		}()

		if err = inputDetailsErr; err != nil {
			Critical.Log("> Error reading console input: ", err)
			continue
		}
		break
	}

	// get password
	for {
		inputPassErr := func() error {
			if password, err = ReadStdin("> Password:", true); err != nil {
				Info.Log("1", err)
				return err
			}
			if err := db.ValidatePassword(password); err != nil {
				Info.Log("2", err)
				return err
			}
			if retypePassword, err = ReadStdin("> Retype Password:", true); err != nil {
				return err
			}
			if password != retypePassword {
				return errors.New("passwords do not match")
			}
			return nil
		}()

		if err = inputPassErr; err != nil {
			Critical.Log("> Error reading console input: ", err)
			continue
		}
		// check entered passwords are the same
		if password != retypePassword {
			Critical.Log("> Passwords do not match!")
			continue
		}
		break
	}

	// perform account creation request to user DB
	for {
		user, err := db.AddUser(forename, surname, email, accountType)
		if err != nil {
			Critical.Logf("> Account creation failed: %s. Try again to create the account.\n\n", err)
			continue
		}

		// set state to ok
		user, ok := db.Users.Get(user.Username)
		if !ok {
			Critical.Logf("> Account creation failed: %s. Try again to create the account.\n\n", "created user was not added to DB")
			continue
		}
		user.AccountState = Registered
		user.PasswordResetRequired = false
		user.TempResetPassword = ""
		db.Users.Set(user.Username, user)
		if err = db.SetNewUserPassword(user.Username, password); err != nil {
			Critical.Logf("> Account creation failed: %s. Try again to create the account.\n\n", errors.Wrap(err, "could not set password"))
			continue
		}
		db.SerializeToFile()
		return
	}
}

// validation regexps
var nameRegex = regexp.MustCompile(`^[A-Za-z ,'-]+$`).MatchString
var emailRegex = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`).MatchString

// AddUser validates the provided user details & adds the user to userDB. A unique username is generated by
// concatenating the forename & surname, then incrementally appending an integer if the username has already been taken.
func (db *UserDB) AddUser(forename string, surname string, email string, userType UserType) (newUser User, sErr *ServerError) {
	// validate inputs
	if len(forename) == 0 || !nameRegex(forename) {
		sErr = &ServerError{errors.New("forename is not valid"), "invalid_forename"}
		return
	}
	if len(surname) == 0 || !nameRegex(surname) {
		sErr = &ServerError{errors.New("surname is not valid"), "invalid_surname"}
		return
	}
	if !emailRegex(email) {
		sErr = &ServerError{errors.New("email is not valid"), "invalid_email"}
		return
	}

	// check if user exists already
	if _, err := db.GetUserByEmail(email); err == nil {
		sErr = &ServerError{errors.New("an account with this email address already exists"), "account_already_exists"}
		return
	}

	newUser = User{
		Email:                 email,
		Type:                  userType,
		Forename:              forename,
		Surname:               surname,
		AccountState:          AwaitingConfirmation,
		CreatedTimestamp:      time.Now().UnixNano(),
		FavouriteFileUUIDs:    make(map[string]bool),
		PasswordResetRequired: true,
	}

	// create username, appending an incremented number if a user exists with that username already (ensures value is unique)
	usernameCounter := 1
	for {
		newUser.Username = newUser.Forename + newUser.Surname
		if usernameCounter > 1 {
			newUser.Username += fmt.Sprintf("%d", usernameCounter)
		}

		// username has not been taken
		if _, ok := db.Users.Get(newUser.Username); !ok {
			break
		}
		// username was taken, increment counter and try again
		usernameCounter++
	}

	// add user to DB
	db.Users.Set(newUser.Username, newUser)
	db.SerializeToFile()
	Creation.Log("new user created: " + newUser.Username)
	return
}

// ValidatePassword validates a password based on the password policy criteria.
func (db *UserDB) ValidatePassword(password string) *ServerError {
	// minimum eight characters, at least one upper case letter, one number and one special character
	var containsUpper, containsLower, containsNumber, containsSpecial bool

	for _, c := range password {
		switch {
		case unicode.IsLower(c):
			containsLower = true
		case unicode.IsNumber(c):
			containsNumber = true
		case unicode.IsUpper(c):
			containsUpper = true
		case unicode.IsPunct(c) || unicode.IsSymbol(c):
			containsSpecial = true
		}
	}

	switch {
	case len(password) < 8:
		return &ServerError{errors.New("password is too short"), "invalid_password_length"}
	case !containsLower:
		return &ServerError{errors.New("password does not contain a lower case character"), "invalid_password_lower"}
	case !containsUpper:
		return &ServerError{errors.New("password does not contain an upper case character"), "invalid_password_upper"}
	case !containsNumber:
		return &ServerError{errors.New("password does not contain a numerical character"), "invalid_password_number"}
	case !containsSpecial:
		return &ServerError{errors.New("password does not contain a special character"), "invalid_password_special"}
	}

	return nil
}

// SetNewUserPassword sets a new password for an existing user. This is used after a user account is created in order to
// complete the registration and after a password reset of an existing account.
func (db *UserDB) SetNewUserPassword(username string, password string) *ServerError {
	// hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	if err != nil {
		Critical.Log(errors.Wrap(err, "password hashing failed"))
		return &ServerError{errors.Wrap(err, "password hashing failed"), "internal_error"}
	}

	user, err := db.GetUserByUsername(username)
	if err != nil {
		return &ServerError{errors.Wrap(err, "user does not exist"), "internal_error"}
	}

	user.Password = string(hashedPassword)
	// destroy/invalidate temp password
	user.TempResetPassword = ""
	user.PasswordResetRequired = false
	user.AccountState = Registered

	db.Users.Set(username, user)
	db.SerializeToFile()
	return nil
}

// AuthenticateUser authenticates a User based on the request session cookie.
func (db *UserDB) AuthenticateUser(r *http.Request) (success bool) {
	session, err := db.cookies.Get(r, "memory-share")
	// no cookie provided
	if err != nil {
		return false
	}

	// check if user is authenticated
	if auth, ok := session.Values["authenticated"].(bool); !ok || !auth {
		return false
	}

	return true
}

// GetSessionUser gets the User corresponding with the request session cookie.
func (db *UserDB) GetSessionUser(r *http.Request) (user User, err error) {
	session, err := db.cookies.Get(r, "memory-share")
	if err != nil {
		return user, errors.Wrap(err, "user has no session cookie")
	}

	return db.GetUserByEmail(session.Values["email"].(string))
}

// SetFavourite adds a file UUID to the favourites list of a user.
func (db *UserDB) SetFavourite(username string, fileUUID string, state bool) (err error) {
	user, ok := db.Users.Get(username)
	if !ok {
		return UserNotFoundError
	}

	favourites := user.FavouriteFileUUIDs
	favourites[fileUUID] = state

	if state == false {
		delete(favourites, fileUUID)
	}

	user.FavouriteFileUUIDs = favourites
	db.Users.Set(username, user)
	db.SerializeToFile()
	return
}

// GetUsers returns a slice copy of all each User from the Users map.
func (db *UserDB) GetUsers() []User {
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

// UserNotFoundError implies no user matched the request.
var UserNotFoundError = errors.New("user not found")

// GetUserByEmail returns the User that matches the given email address.
func (db *UserDB) GetUserByEmail(email string) (User, error) {
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
		return user, UserNotFoundError
	}
	return user, nil
}

// GetUserByUsername returns the User that matches the given username.
func (db *UserDB) GetUserByUsername(username string) (user User, err error) {
	user, ok := db.Users.Get(username)
	if !ok {
		err = UserNotFoundError
	}
	return
}

// LoginUser handles logging in users.
func (db *UserDB) LoginUser(w http.ResponseWriter, r *http.Request) (success bool, err error) {
	session, _ := db.cookies.Get(r, "memory-share")

	if err = r.ParseForm(); err != nil {
		return false, errors.Wrap(err, "error parsing form")
	}

	emailParam, passwordParam := r.FormValue("email"), r.FormValue("password")

	// check to see if a user corresponds with email address
	user, err := db.GetUserByEmail(emailParam)
	if err != nil {
		return false, nil
	}

	// user with email found
	loggedIn := func() bool {
		// compare stored hash against hash of input password
		if user.Password != "" {
			if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(passwordParam)); err == nil {
				user.PasswordResetRequired = false
				user.TempResetPassword = ""
				return true
			}
		}

		// check against temp reset password
		if user.TempResetPassword != "" && time.Since(user.PasswordResetTimestamp).Hours() < 1 {
			if err := bcrypt.CompareHashAndPassword([]byte(user.TempResetPassword), []byte(passwordParam)); err == nil {
				user.PasswordResetRequired = true
				return true
			}
		}

		return false
	}()

	// login failed
	if loggedIn == false {
		return false, nil
	}

	// record login
	user.LoginTimestamp = time.Now().UnixNano()
	user.LoginCount++
	db.Users.Set(user.Username, user)
	db.SerializeToFile()

	// set user as authenticated
	session.Values["authenticated"] = true
	session.Values["email"] = emailParam
	// session expires the number of days specified in the config
	session.Options = &sessions.Options{
		Path:   "/",
		MaxAge: 86400 * config.MaxSessionAge,
	}
	if err := session.Save(r, w); err != nil {
		return false, errors.Wrap(err, "error saving session")
	}

	return true, nil
}

// LogoutUser handles logging out users.
func (db *UserDB) LogoutUser(w http.ResponseWriter, r *http.Request) (err error) {
	session, err := db.cookies.Get(r, "memory-share")
	if err != nil {
		return errors.Wrap(err, "failed to fetch session cookie")
	}

	// revoke user's authentication
	session.Values["authenticated"] = false
	session.Options.MaxAge = -1
	if err = session.Save(r, w); err != nil {
		return errors.Wrap(err, "error saving session")
	}
	return nil
}

// this list of chars are randomly selected from and included in random reset/registration temp passwords
var randomPassChars = []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!$%^&*()@#?")

// SetTempPassword sets a temporary password for a user and returns it.
func (db *UserDB) SetTempPassword(email string) (tempPass string, err error) {
	user, err := db.GetUserByEmail(email)
	if err != nil {
		return
	}

	// generate random password
	tempPass = uniuri.NewLenChars(15, randomPassChars)

	// hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(tempPass), 14)
	if err != nil {
		Critical.Log("error hashing password")
		return
	}
	user.TempResetPassword = string(hashedPassword)
	user.PasswordResetTimestamp = time.Now()

	db.Users.Set(user.Username, user)
	db.SerializeToFile()
	return
}

// FetchSessionKey gets the session secure key from session_key.dat if one was created in the previous run, otherwise
// it creates a new one.
func FetchSessionKey() (key []byte, err error) {
	sessionFilePath := config.rootPath + "/config/session_key.dat"

	// check if file exists
	ok, err := FileOrDirExists(sessionFilePath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to check session key file")
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
			return nil, errors.New("could not generate session key")
		}

		// encode to file
		encoder := gob.NewEncoder(file)
		if err = encoder.Encode(&key); err != nil {
			return nil, errors.Wrap(err, "failed to save session key to file")
		}

		return key, nil
	}

	// open file to read from
	file, err := os.Open(sessionFilePath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open session key file")
	}
	defer file.Close()

	// decode file contents to store map
	decoder := gob.NewDecoder(file)
	if err = decoder.Decode(&key); err != nil {
		return nil, errors.Wrap(err, "failed to decode session key from file")
	}

	return key, nil
}

// SerializeToFile serializes the entire UserDB to a file on disk via gob.
func (db *UserDB) SerializeToFile() (err error) {
	// create/truncate file for writing to
	file, err := os.Create(db.file)
	if err != nil {
		Critical.Log(err)
		return err
	}
	db.Users.mu.Lock()
	defer db.Users.mu.Unlock()
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

// DeserializeFromFile deserializes a file to the UserDB structure, overwriting current map values.
func (db *UserDB) DeserializeFromFile() (err error) {
	db.Users.mu.Lock()

	// if db file does not exist, create a new one
	if _, err := os.Stat(db.file); os.IsNotExist(err) {
		db.Users.mu.Unlock()
		db.SerializeToFile()
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
