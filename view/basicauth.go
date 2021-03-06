package view

import (
	"encoding/base64"
	// "fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/ungerik/go-start/utils"
)

func BasicAuthFromRequest(request *Request) (username, password string) {
	header := request.Header.Get("Authorization")
	f := strings.Fields(header)
	if len(f) == 2 && f[0] == "Basic" {
		if b, err := base64.StdEncoding.DecodeString(f[1]); err == nil {
			a := strings.Split(string(b), ":")
			if len(a) == 2 {
				username = a[0]
				password = a[1]
				return username, password
			}
		}
	}
	return "", ""
}

func SendBasicAuthRequired(response *Response, realm string) {
	response.Header().Set("WWW-Authenticate", "Basic realm=\""+realm+"\"")
	response.AuthorizationRequired401()
	log.Printf("BasicAuth requested for realm '%s'", realm)
}

///////////////////////////////////////////////////////////////////////////////
// BasicAuth

// BasicAuth implements HTTP basic auth as Authenticator.
// See also HtpasswdWatchingBasicAuth
type BasicAuth struct {
	Realm          string
	UserPassword   map[string]string // map of username to base64 encoded SHA1 hash of the password
	loggedOutUsers map[string]bool
	mutex          sync.Mutex
}

// NewBasicAuth creates a BasicAuth instance with a series of
// not encrypted usernames and passwords that are provided in alternating order.
func NewBasicAuth(realm string, usernamesAndPasswords ...string) *BasicAuth {
	userPass := make(map[string]string, len(usernamesAndPasswords)/2)

	for i := 0; i < len(usernamesAndPasswords)/2; i++ {
		username := usernamesAndPasswords[i*2]
		password := usernamesAndPasswords[i*2+1]
		userPass[username] = utils.SHA1Base64String(password)
	}

	return &BasicAuth{
		Realm:          realm,
		UserPassword:   userPass,
		loggedOutUsers: make(map[string]bool),
	}
}

func (basicAuth *BasicAuth) Authenticate(ctx *Context) (ok bool, err error) {
	basicAuth.mutex.Lock()
	defer basicAuth.mutex.Unlock()

	username, password := BasicAuthFromRequest(ctx.Request)
	if username != "" {
		if basicAuth.loggedOutUsers[username] {
			basicAuth.loggedOutUsers[username] = false
			SendBasicAuthRequired(ctx.Response, basicAuth.Realm)
			return false, nil
		}

		passHash, hasUser := basicAuth.UserPassword[username]
		if hasUser && passHash == utils.SHA1Base64String(password) {
			ctx.AuthUser = username
			// fmt.Println("BasicAuth", username)
			return true, nil
		}
	}

	ctx.AuthUser = ""
	SendBasicAuthRequired(ctx.Response, basicAuth.Realm)
	return false, nil
}

func (basicAuth *BasicAuth) Logout(ctx *Context) {
	if ctx.AuthUser != "" {
		basicAuth.mutex.Lock()
		basicAuth.loggedOutUsers[ctx.AuthUser] = true
		basicAuth.mutex.Unlock()

		log.Println("Logged out user", ctx.AuthUser)

		ctx.AuthUser = ""
	}
}

///////////////////////////////////////////////////////////////////////////////
// HtpasswdWatchingBasicAuth

type HtpasswdWatchingBasicAuth struct {
	BasicAuth
	htpasswdFile     string
	htpasswdFileTime time.Time
}

func NewHtpasswdWatchingBasicAuth(realm, htpasswdFile string) (auth *HtpasswdWatchingBasicAuth, err error) {
	auth = &HtpasswdWatchingBasicAuth{
		BasicAuth:    BasicAuth{Realm: realm, loggedOutUsers: make(map[string]bool)},
		htpasswdFile: htpasswdFile,
	}
	auth.UserPassword, auth.htpasswdFileTime, err = utils.ReadHtpasswdFile(htpasswdFile)
	if err != nil {
		return nil, err
	}
	return auth, nil
}

func (auth *HtpasswdWatchingBasicAuth) Authenticate(ctx *Context) (ok bool, err error) {
	auth.mutex.Lock()

	t, err := utils.FileModifiedTime(auth.htpasswdFile)
	if err != nil {
		auth.mutex.Unlock()
		return false, err
	}

	if t.After(auth.htpasswdFileTime) {
		auth.UserPassword, auth.htpasswdFileTime, err = utils.ReadHtpasswdFile(auth.htpasswdFile)
		if err != nil {
			auth.mutex.Unlock()
			return false, err
		}
	}

	auth.mutex.Unlock()

	return auth.BasicAuth.Authenticate(ctx)
}
