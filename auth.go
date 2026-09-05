package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

const authFile = "auth.txt"
const minPasswordLen = 4

const sessionCookie = "session"
const sessionLifetime = 30 * 24 * time.Hour

// authPassword is the access password, loaded (or set) once at startup
// before the server begins serving, so it needs no locking.
var authPassword string

// sessions holds the random tokens handed out on successful logins, each
// mapped to its expiry time. Tokens are added on login and removed on
// logout or once expired.
var (
	sessionsMu sync.Mutex
	sessions   = make(map[string]time.Time)
)

// loadOrSetupPassword loads the access password from auth.txt.
// On first run (missing or empty file) it prompts the user in the terminal
// to set their own password and saves it to auth.txt.
func loadOrSetupPassword() error {
	data, err := os.ReadFile(authFile)
	if err == nil {
		password := strings.TrimSpace(string(data))
		if password == "" {
			log.Printf("Auth: %s is empty, prompting for a new password", authFile)
			return promptAndSavePassword()
		}
		authPassword = password
		log.Println("Auth: password loaded from", authFile)
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return promptAndSavePassword()
}

// promptAndSavePassword runs the one-time setup: it asks for a password
// twice (input hidden) and saves it to auth.txt. It requires an interactive
// terminal so a non-TTY start (e.g. a service) fails loudly instead of
// hanging on input.
func promptAndSavePassword() error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("first-run setup needs an interactive terminal: run the server in a terminal once, or create " + authFile + " with the password on a single line")
	}
	for {
		fmt.Printf("Set your access password (at least %d characters): ", minPasswordLen)
		password, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return err
		}
		if len(password) < minPasswordLen {
			fmt.Printf("Too short — use at least %d characters.\n", minPasswordLen)
			continue
		}

		fmt.Print("Confirm password: ")
		confirm, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return err
		}
		if string(password) != string(confirm) {
			fmt.Println("Passwords do not match, try again.")
			continue
		}

		if err := os.WriteFile(authFile, []byte(string(password)+"\n"), 0600); err != nil {
			return err
		}
		authPassword = string(password)
		log.Printf("Auth: password saved to %s", authFile)
		return nil
	}
}

// LoginData is the data for the login page template.
type LoginData struct {
	Invalid bool
}

// handleLogin renders the login form on GET and checks the submitted
// password on POST. There is a single user, so only the password is asked
// for. A correct password starts a session (cookie) and redirects to the
// gallery; a wrong one re-renders the form with an error.
func handleLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if sessionValid(r) {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		tmpls.ExecuteTemplate(w, "login.html", LoginData{})

	case http.MethodPost:
		password := r.FormValue("password")
		if subtle.ConstantTimeCompare([]byte(password), []byte(authPassword)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			tmpls.ExecuteTemplate(w, "login.html", LoginData{Invalid: true})
			return
		}
		token, err := newSession()
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookie,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   int(sessionLifetime.Seconds()),
		})
		http.Redirect(w, r, "/", http.StatusFound)

	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleLogout drops the caller's session and sends them back to the login
// page. POST only, so a stray link cannot log the user out.
func handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		sessionsMu.Lock()
		delete(sessions, cookie.Value)
		sessionsMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/login", http.StatusFound)
}

// newSession creates a random session token and records it with its expiry.
func newSession() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)

	sessionsMu.Lock()
	sessions[token] = time.Now().Add(sessionLifetime)
	sessionsMu.Unlock()
	return token, nil
}

// sessionValid reports whether the request carries a live session cookie.
// Expired sessions are dropped when noticed.
func sessionValid(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}

	sessionsMu.Lock()
	expiry, ok := sessions[cookie.Value]
	if ok && time.Now().After(expiry) {
		delete(sessions, cookie.Value)
		ok = false
	}
	sessionsMu.Unlock()
	return ok
}

// authMiddleware rejects every request without a valid session: API calls
// get a plain 401, browser navigation is redirected to the login page.
// The login and logout routes themselves are always reachable.
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login", "/logout":
			next.ServeHTTP(w, r)
			return
		}
		if sessionValid(r) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	})
}
