package main

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"golang.org/x/term"
)

const authFile = "auth.txt"
const minPasswordLen = 4

// authPassword is the access password, loaded (or set) once at startup
// before the server begins serving, so it needs no locking.
var authPassword string

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

// authMiddleware rejects every request that does not present the access
// password via HTTP Basic Auth. The username is ignored; only the password
// matters. The browser shows its native prompt and then attaches the
// credentials to all same-origin requests, so the UI works unchanged.
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, password, ok := r.BasicAuth()
		if ok && subtle.ConstantTimeCompare([]byte(password), []byte(authPassword)) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="go-share"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}
