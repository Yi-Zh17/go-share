package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAuthMiddlewareRedirectsToLoginWithoutSession(t *testing.T) {
	handler := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect without a session, got %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/login" {
		t.Fatalf("unexpected redirect target: %q", got)
	}
}

func TestAuthMiddlewareRejectsAPIWithoutSession(t *testing.T) {
	handler := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/folders", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an API call without a session, got %d", rec.Code)
	}
}

func TestAuthMiddlewareAcceptsValidSession(t *testing.T) {
	handler := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	token, err := newSession()
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with a valid session, got %d", rec.Code)
	}
}

func TestAuthMiddlewareRejectsUnknownSessionToken(t *testing.T) {
	handler := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "not-a-real-token"})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect with an unknown token, got %d", rec.Code)
	}
}

func TestHandleLoginWrongPassword(t *testing.T) {
	authPassword = "secret"

	form := url.Values{"password": {"wrong"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	handleLogin(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with a wrong password, got %d", rec.Code)
	}
	if cookies := rec.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("expected no session cookie on a failed login, got %v", cookies)
	}
}

func TestHandleLoginSetsSessionOnCorrectPassword(t *testing.T) {
	authPassword = "secret"

	form := url.Values{"password": {"secret"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	handleLogin(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect after login, got %d", rec.Code)
	}

	// Replay the cookies from the login response on a follow-up request.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, cookie := range rec.Result().Cookies() {
		req2.AddCookie(cookie)
	}
	if !sessionValid(req2) {
		t.Fatal("expected the cookie set by login to be a valid session")
	}
}

func TestHandleLogoutEndsSession(t *testing.T) {
	token, err := newSession()
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})

	rec := httptest.NewRecorder()
	handleLogout(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect after logout, got %d", rec.Code)
	}
	if _, ok := sessions[token]; ok {
		t.Fatal("expected the session to be removed on logout")
	}
}
