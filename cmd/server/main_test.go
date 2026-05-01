package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestValidateSessionToken(t *testing.T) {
	app := &application{
		accessPassword: "top-secret",
		sessionSecret:  []byte("0123456789abcdef0123456789abcdef"),
	}

	validToken := app.issueSessionToken(time.Now().UTC().Add(5 * time.Minute))
	if !app.validateSessionToken(validToken) {
		t.Fatal("expected issued token to be valid")
	}

	expiredToken := app.issueSessionToken(time.Now().UTC().Add(-5 * time.Minute))
	if app.validateSessionToken(expiredToken) {
		t.Fatal("expected expired token to be invalid")
	}

	if app.validateSessionToken("not-a-valid-token") {
		t.Fatal("expected malformed token to be invalid")
	}
}

func TestRequireAuthentication(t *testing.T) {
	app := &application{
		accessPassword: "top-secret",
		sessionSecret:  []byte("0123456789abcdef0123456789abcdef"),
	}

	protected := app.requireAuthentication(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	unauthorizedRequest := httptest.NewRequest(http.MethodGet, "/api/videos", nil)
	unauthorizedRecorder := httptest.NewRecorder()
	protected.ServeHTTP(unauthorizedRecorder, unauthorizedRequest)

	if unauthorizedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized status, got %d", unauthorizedRecorder.Code)
	}

	authorizedRequest := httptest.NewRequest(http.MethodGet, "/api/videos", nil)
	authorizedRequest.AddCookie(&http.Cookie{
		Name:  sessionCookie,
		Value: app.issueSessionToken(time.Now().UTC().Add(5 * time.Minute)),
	})

	authorizedRecorder := httptest.NewRecorder()
	protected.ServeHTTP(authorizedRecorder, authorizedRequest)

	if authorizedRecorder.Code != http.StatusNoContent {
		t.Fatalf("expected protected handler to run, got %d", authorizedRecorder.Code)
	}
}

func TestHandleAuthLoginRateLimited(t *testing.T) {
	app := &application{
		accessPassword: "top-secret",
		sessionSecret:  []byte("0123456789abcdef0123456789abcdef"),
	}

	for attempt := 1; attempt < loginRateLimitMaxFails; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"wrong"}`))
		request.Header.Set("Content-Type", "application/json")
		request.RemoteAddr = "127.0.0.1:3000"

		recorder := httptest.NewRecorder()
		app.handleAuthLogin(recorder, request)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", attempt, recorder.Code)
		}
	}

	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"wrong"}`))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "127.0.0.1:3000"

	recorder := httptest.NewRecorder()
	app.handleAuthLogin(recorder, request)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after too many failures, got %d", recorder.Code)
	}

	if recorder.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header to be set")
	}
}

func TestSeparatedFrontendRoutes(t *testing.T) {
	app := &application{
		accessPassword: "top-secret",
		sessionSecret:  []byte("0123456789abcdef0123456789abcdef"),
		frontendFS: fstest.MapFS{
			"login/index.html": &fstest.MapFile{Data: []byte("<html><head></head><body>login</body></html>")},
			"app/index.html":   &fstest.MapFile{Data: []byte("<html><head></head><body>app</body></html>")},
		},
	}

	homeRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	homeRecorder := httptest.NewRecorder()
	app.handleHome(homeRecorder, homeRequest)

	if homeRecorder.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected home redirect, got %d", homeRecorder.Code)
	}
	if location := homeRecorder.Header().Get("Location"); location != loginPagePath {
		t.Fatalf("expected redirect to %s, got %s", loginPagePath, location)
	}

	loginRequest := httptest.NewRequest(http.MethodGet, loginPagePath, nil)
	loginRecorder := httptest.NewRecorder()
	app.handleLoginPage(loginRecorder, loginRequest)

	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("expected login page to render, got %d", loginRecorder.Code)
	}
	if !strings.Contains(loginRecorder.Body.String(), "login") {
		t.Fatal("expected login page html to be served")
	}

	appRequest := httptest.NewRequest(http.MethodGet, appPagePath, nil)
	appRecorder := httptest.NewRecorder()
	app.handleAppPage(appRecorder, appRequest)

	if appRecorder.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected unauthenticated app redirect, got %d", appRecorder.Code)
	}
	if location := appRecorder.Header().Get("Location"); location != loginPagePath {
		t.Fatalf("expected app redirect to %s, got %s", loginPagePath, location)
	}

	authenticatedRequest := httptest.NewRequest(http.MethodGet, appPagePath, nil)
	authenticatedRequest.AddCookie(&http.Cookie{
		Name:  sessionCookie,
		Value: app.issueSessionToken(time.Now().UTC().Add(5 * time.Minute)),
	})
	authenticatedRecorder := httptest.NewRecorder()
	app.handleAppPage(authenticatedRecorder, authenticatedRequest)

	if authenticatedRecorder.Code != http.StatusOK {
		t.Fatalf("expected authenticated app page to render, got %d", authenticatedRecorder.Code)
	}
	if !strings.Contains(authenticatedRecorder.Body.String(), "app") {
		t.Fatal("expected app html to be served")
	}
}

func TestServeFrontendHTMLInjectsBaseHref(t *testing.T) {
	app := &application{
		basePath: "/douyin",
		frontendFS: fstest.MapFS{
			"login/index.html": &fstest.MapFile{Data: []byte("<html><head></head><body></body></html>")},
		},
	}

	recorder := httptest.NewRecorder()
	app.serveFrontendHTML(recorder, "login/index.html", app.routePath(loginPagePath))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected frontend html to render, got %d", recorder.Code)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, `<base href="/douyin/login/" />`) {
		t.Fatalf("expected base href injection, got %q", body)
	}
}

var _ fs.FS = fstest.MapFS{}
