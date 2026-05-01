package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	assets "douyin"
	"douyin/internal/downloader"
	"douyin/internal/store"
)

const (
	defaultPort    = "8080"
	sqlitePath     = "data/videos.db"
	legacyJSONPath = "data/videos.json"
	videoDataDir   = "data/videos"
	coverDataDir   = "data/covers"
	frontendDist   = "frontend/dist"
	maxShareBytes  = 8 * 1024
	sessionMaxAge  = 7 * 24 * time.Hour
	sessionCookie  = "douyin_session"
	loginPagePath  = "/login/"
	appPagePath    = "/app/"

	loginRateLimitWindow   = 10 * time.Minute
	loginRateLimitMaxFails = 5
	loginRateLimitLockout  = 15 * time.Minute
	loginRateLimitTTL      = 24 * time.Hour
)

type application struct {
	rootDir         string
	basePath        string
	accessPassword  string
	sessionSecret   []byte
	store           *store.Store
	frontendHandler http.Handler
	frontendFS      fs.FS
	loginLimitMu    sync.Mutex
	loginLimits     map[string]loginAttemptState
}

type downloadRequest struct {
	ShareText string `json:"shareText"`
}

type loginRequest struct {
	Password string `json:"password"`
}

type ratingRequest struct {
	Rating int `json:"rating"`
}

type tagRequest struct {
	Name string `json:"name"`
}

type videoTagsRequest struct {
	TagIDs []int64 `json:"tagIds"`
}

type deleteResponse struct {
	Deleted bool   `json:"deleted"`
	VideoID string `json:"videoId"`
}

type authSessionResponse struct {
	Enabled       bool `json:"enabled"`
	Authenticated bool `json:"authenticated"`
}

type loginAttemptState struct {
	Failures    int
	WindowStart time.Time
	LockedUntil time.Time
	LastSeenAt  time.Time
}

func main() {
	rootDir, err := executableDir()
	if err != nil {
		log.Fatalf("resolve executable directory: %v", err)
	}
	basePath := normalizeBasePath(os.Getenv("BASE_PATH"))
	accessPassword := strings.TrimSpace(os.Getenv("LOGIN_PASSWORD"))
	sessionSecret, err := generateSessionSecret()
	if err != nil {
		log.Fatalf("generate session secret: %v", err)
	}

	for _, dir := range []string{videoDataDir, coverDataDir} {
		if err := os.MkdirAll(filepath.Join(rootDir, dir), 0o755); err != nil {
			log.Fatalf("create data directory %s: %v", dir, err)
		}
	}

	frontendFS, err := fs.Sub(assets.FrontendDist, frontendDist)
	if err != nil {
		log.Fatalf("load embedded frontend assets: %v", err)
	}

	videoStore, err := store.New(filepath.Join(rootDir, sqlitePath), filepath.Join(rootDir, legacyJSONPath))
	if err != nil {
		log.Fatalf("open metadata store: %v", err)
	}
	defer func() {
		if err := videoStore.Close(); err != nil {
			log.Printf("close sqlite database: %v", err)
		}
	}()

	app := &application{
		rootDir:         rootDir,
		basePath:        basePath,
		accessPassword:  accessPassword,
		sessionSecret:   sessionSecret,
		store:           videoStore,
		frontendHandler: http.FileServer(http.FS(frontendFS)),
		frontendFS:      frontendFS,
	}

	mux := http.NewServeMux()
	mux.HandleFunc(app.routePath("/api/health"), app.handleHealth)
	mux.HandleFunc(app.routePath("/api/auth/session"), app.handleAuthSession)
	mux.HandleFunc(app.routePath("/api/auth/login"), app.handleAuthLogin)
	mux.HandleFunc(app.routePath("/api/auth/logout"), app.handleAuthLogout)
	mux.Handle(app.routePath("/api/tags"), app.requireAuthentication(http.HandlerFunc(app.handleTags)))
	mux.Handle(app.routePath("/api/tags/"), app.requireAuthentication(http.HandlerFunc(app.handleTagByID)))
	mux.Handle(app.routePath("/api/videos"), app.requireAuthentication(http.HandlerFunc(app.handleVideos)))
	mux.Handle(app.routePath("/api/videos/"), app.requireAuthentication(http.HandlerFunc(app.handleVideoByID)))
	mux.Handle(
		app.routePath("/media/"),
		app.requireAuthentication(
			http.StripPrefix(app.routePath("/media/"), http.FileServer(http.Dir(filepath.Join(rootDir, videoDataDir)))),
		),
	)
	mux.Handle(
		app.routePath("/covers/"),
		app.requireAuthentication(
			http.StripPrefix(app.routePath("/covers/"), http.FileServer(http.Dir(filepath.Join(rootDir, coverDataDir)))),
		),
	)
	mux.Handle(app.routePath("/assets/"), http.StripPrefix(app.baseHref(), app.frontendHandler))
	if app.basePath != "" {
		mux.HandleFunc(app.basePath, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, app.routePath("/"), http.StatusTemporaryRedirect)
		})
	}
	mux.HandleFunc(app.routePath(strings.TrimRight(loginPagePath, "/")), func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, app.routePath(loginPagePath), http.StatusTemporaryRedirect)
	})
	mux.HandleFunc(app.routePath(loginPagePath), app.handleLoginPage)
	mux.HandleFunc(app.routePath(strings.TrimRight(appPagePath, "/")), func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, app.routePath(appPagePath), http.StatusTemporaryRedirect)
	})
	mux.HandleFunc(app.routePath(appPagePath), app.handleAppPage)
	mux.HandleFunc(app.routePath("/"), app.handleHome)

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = defaultPort
	}

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           withJSONLogging(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("server listening on http://localhost:%s", port)
	log.Printf("base path: %s", firstNonEmpty(app.basePath, "/"))
	log.Printf("login password enabled: %t", app.authEnabled())
	log.Printf("data directory: %s", filepath.Join(rootDir, "data"))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
}

func executableDir() (string, error) {
	executablePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}

	if resolvedPath, err := filepath.EvalSymlinks(executablePath); err == nil {
		executablePath = resolvedPath
	}

	return filepath.Dir(executablePath), nil
}

func (app *application) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (app *application) handleAuthSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, authSessionResponse{
		Enabled:       app.authEnabled(),
		Authenticated: app.isAuthenticated(r),
	})
}

func (app *application) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !app.authEnabled() {
		writeJSON(w, http.StatusOK, authSessionResponse{
			Enabled:       false,
			Authenticated: true,
		})
		return
	}

	clientKey := loginClientKey(r)
	if limited, retryAfter := app.loginRateLimitStatus(clientKey); limited {
		writeLoginRateLimited(w, retryAfter)
		return
	}

	var body loginRequest
	if err := decodeJSONBody(r, &body, 2048); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(body.Password)), []byte(app.accessPassword)) != 1 {
		if limited, retryAfter := app.recordFailedLogin(clientKey); limited {
			writeLoginRateLimited(w, retryAfter)
			return
		}

		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "密码不正确"})
		return
	}

	app.clearLoginRateLimit(clientKey)
	app.setSessionCookie(w, r)
	writeJSON(w, http.StatusOK, authSessionResponse{
		Enabled:       true,
		Authenticated: true,
	})
}

func (app *application) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	app.clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, authSessionResponse{
		Enabled:       app.authEnabled(),
		Authenticated: false,
	})
}

func (app *application) handleTags(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tags, err := app.store.ListTags()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
	case http.MethodPost:
		var body tagRequest
		if err := decodeJSONBody(r, &body, 2048); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		tag, err := app.store.CreateTag(body.Name)
		switch {
		case errors.Is(err, store.ErrTagExists):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "tag already exists"})
		case err != nil:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusCreated, map[string]any{"tag": tag})
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (app *application) handleTagByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rawTagID := strings.Trim(strings.TrimPrefix(app.stripBasePath(pathClean(r.URL.Path)), "/api/tags/"), "/")
	if rawTagID == "" {
		http.NotFound(w, r)
		return
	}

	tagID, err := strconv.ParseInt(rawTagID, 10, 64)
	if err != nil || tagID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid tag id"})
		return
	}

	switch err := app.store.DeleteTag(tagID); {
	case errors.Is(err, store.ErrTagNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "tag not found"})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "tagId": tagID})
	}
}

func (app *application) handleVideos(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		filter, err := parseListFilter(r.URL.Query().Get("rating"), r.URL.Query().Get("tags"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		records, err := app.store.List(filter)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"videos": app.presentVideos(records),
		})
	case http.MethodPost:
		app.handleDownloadVideo(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (app *application) handleVideoByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(app.stripBasePath(pathClean(r.URL.Path)), "/api/videos/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	switch {
	case r.Method == http.MethodPatch && len(parts) == 2 && parts[1] == "rating":
		app.handleUpdateRating(w, r, parts[0])
	case r.Method == http.MethodPatch && len(parts) == 2 && parts[1] == "tags":
		app.handleUpdateTags(w, r, parts[0])
	case r.Method == http.MethodDelete && len(parts) == 1:
		app.handleDeleteVideo(w, r, parts[0])
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (app *application) handleUpdateTags(w http.ResponseWriter, r *http.Request, videoID string) {
	var body videoTagsRequest
	if err := decodeJSONBody(r, &body, 8*1024); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	record, err := app.store.SetVideoTags(videoID, body.TagIDs)
	switch {
	case errors.Is(err, os.ErrNotExist):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "video not found"})
	case errors.Is(err, store.ErrUnknownTag):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "one or more tags do not exist"})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"video": app.presentVideo(record)})
	}
}

func (app *application) handleUpdateRating(w http.ResponseWriter, r *http.Request, videoID string) {
	var body ratingRequest
	if err := decodeJSONBody(r, &body, 1024); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if body.Rating < 1 || body.Rating > 5 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "rating must be between 1 and 5"})
		return
	}

	record, err := app.store.UpdateRating(videoID, body.Rating)
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "video not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"video": app.presentVideo(record)})
}

func (app *application) handleDeleteVideo(w http.ResponseWriter, _ *http.Request, videoID string) {
	record, err := app.store.Get(videoID)
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "video not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	absoluteFile := filepath.Join(app.rootDir, filepath.FromSlash(record.LocalFile))
	if err := os.Remove(absoluteFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("delete local video file: %v", err)})
		return
	}

	if record.CoverLocalFile != "" {
		absoluteCoverFile := filepath.Join(app.rootDir, filepath.FromSlash(record.CoverLocalFile))
		if err := os.Remove(absoluteCoverFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("delete local cover file: %v", err)})
			return
		}
	}

	if err := app.store.Delete(videoID); errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "video not found"})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, deleteResponse{
		Deleted: true,
		VideoID: videoID,
	})
}

func (app *application) handleDownloadVideo(w http.ResponseWriter, r *http.Request) {
	var body downloadRequest
	if err := decodeJSONBody(r, &body, maxShareBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	shareText := strings.TrimSpace(body.ShareText)
	if shareText == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "shareText is required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	extracted, err := downloader.Extract(ctx, shareText)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	if existing, err := app.store.Get(extracted.VideoID); err == nil {
		if _, err := os.Stat(filepath.Join(app.rootDir, existing.LocalFile)); err == nil {
			existing = mergeRecordWithExtractedMetadata(existing, extracted, shareText)

			if updated, updateErr := app.backfillLocalCover(ctx, existing, extracted.CoverURL); updateErr == nil {
				existing = updated
			} else {
				log.Printf("backfill cover for %s: %v", existing.VideoID, updateErr)
			}

			existing, err = app.store.Upsert(existing)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}

			writeJSON(w, http.StatusOK, map[string]any{
				"video":         app.presentVideo(existing),
				"alreadyStored": true,
				"downloadedNow": false,
			})
			return
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	fileName := extracted.VideoID + ".mp4"
	relativeFile := filepath.ToSlash(filepath.Join(videoDataDir, fileName))
	absoluteFile := filepath.Join(app.rootDir, videoDataDir, fileName)

	fileSize, err := downloader.DownloadToFile(ctx, extracted.VideoDownloadURL, absoluteFile)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	coverLocalFile, coverLocalURL, coverSourceURL, coverErr := app.storeCoverLocally(ctx, extracted.VideoID, extracted.CoverURL)
	if coverErr != nil {
		log.Printf("store cover for %s: %v", extracted.VideoID, coverErr)
	}

	displayCoverURL := firstNonEmpty(coverLocalURL, coverSourceURL)

	record := store.VideoRecord{
		ID:              extracted.VideoID,
		VideoID:         extracted.VideoID,
		Title:           extracted.Title,
		Description:     extracted.Description,
		Author:          extracted.Author,
		AuthorID:        extracted.AuthorID,
		ShareURL:        extracted.ShareURL,
		OriginalURL:     extracted.OriginalURL,
		ContentType:     extracted.ContentType,
		CoverURL:        displayCoverURL,
		CoverSourceURL:  coverSourceURL,
		CoverLocalFile:  coverLocalFile,
		CoverLocalURL:   coverLocalURL,
		VideoURI:        extracted.VideoURI,
		DownloadURL:     extracted.VideoDownloadURL,
		WatermarkURL:    extracted.WatermarkVideoURL,
		VideoWidth:      extracted.VideoWidth,
		VideoHeight:     extracted.VideoHeight,
		Duration:        extracted.Duration,
		LikeCount:       extracted.LikeCount,
		CommentCount:    extracted.CommentCount,
		ShareCount:      extracted.ShareCount,
		CollectCount:    extracted.CollectCount,
		LocalFile:       relativeFile,
		LocalURL:        app.routePath("/media/" + fileName),
		FileSize:        fileSize,
		LastSourceInput: shareText,
		SavedAt:         time.Now().UTC(),
	}

	record, err = app.store.Upsert(record)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"video":         app.presentVideo(record),
		"alreadyStored": false,
		"downloadedNow": true,
	})
}

func (app *application) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if pathClean(r.URL.Path) != pathClean(app.routePath("/")) {
		http.NotFound(w, r)
		return
	}

	target := app.routePath(appPagePath)
	if app.authEnabled() && !app.isAuthenticated(r) {
		target = app.routePath(loginPagePath)
	}

	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
}

func (app *application) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if pathClean(r.URL.Path) != pathClean(app.routePath(loginPagePath)) {
		http.NotFound(w, r)
		return
	}

	if !app.authEnabled() || app.isAuthenticated(r) {
		http.Redirect(w, r, app.routePath(appPagePath), http.StatusTemporaryRedirect)
		return
	}

	app.serveFrontendHTML(w, "login/index.html", app.routePath(loginPagePath))
}

func (app *application) handleAppPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if pathClean(r.URL.Path) != pathClean(app.routePath(appPagePath)) {
		http.NotFound(w, r)
		return
	}

	if app.authEnabled() && !app.isAuthenticated(r) {
		http.Redirect(w, r, app.routePath(loginPagePath), http.StatusTemporaryRedirect)
		return
	}

	app.serveFrontendHTML(w, "app/index.html", app.routePath(appPagePath))
}

func (app *application) authEnabled() bool {
	return strings.TrimSpace(app.accessPassword) != ""
}

func (app *application) loginRateLimitStatus(clientKey string) (bool, time.Duration) {
	if clientKey == "" {
		return false, 0
	}

	app.loginLimitMu.Lock()
	defer app.loginLimitMu.Unlock()

	now := time.Now().UTC()
	app.pruneLoginLimitLocked(now)

	state, ok := app.loginLimits[clientKey]
	if !ok {
		return false, 0
	}

	if state.LockedUntil.After(now) {
		return true, state.LockedUntil.Sub(now)
	}

	if state.WindowStart.IsZero() || now.Sub(state.WindowStart) > loginRateLimitWindow {
		delete(app.loginLimits, clientKey)
	}

	return false, 0
}

func (app *application) recordFailedLogin(clientKey string) (bool, time.Duration) {
	if clientKey == "" {
		return false, 0
	}

	app.loginLimitMu.Lock()
	defer app.loginLimitMu.Unlock()

	now := time.Now().UTC()
	app.pruneLoginLimitLocked(now)
	if app.loginLimits == nil {
		app.loginLimits = make(map[string]loginAttemptState)
	}

	state := app.loginLimits[clientKey]
	if state.WindowStart.IsZero() || now.Sub(state.WindowStart) > loginRateLimitWindow || (!state.LockedUntil.IsZero() && state.LockedUntil.Before(now)) {
		state = loginAttemptState{
			Failures:    0,
			WindowStart: now,
		}
	}

	state.Failures++
	state.LastSeenAt = now

	if state.Failures >= loginRateLimitMaxFails {
		state.LockedUntil = now.Add(loginRateLimitLockout)
		app.loginLimits[clientKey] = state
		return true, state.LockedUntil.Sub(now)
	}

	app.loginLimits[clientKey] = state
	return false, 0
}

func (app *application) clearLoginRateLimit(clientKey string) {
	if clientKey == "" {
		return
	}

	app.loginLimitMu.Lock()
	defer app.loginLimitMu.Unlock()

	delete(app.loginLimits, clientKey)
}

func (app *application) pruneLoginLimitLocked(now time.Time) {
	if app.loginLimits == nil {
		return
	}

	for key, state := range app.loginLimits {
		if state.LastSeenAt.IsZero() {
			delete(app.loginLimits, key)
			continue
		}
		if now.Sub(state.LastSeenAt) > loginRateLimitTTL {
			delete(app.loginLimits, key)
		}
	}
}

func (app *application) requireAuthentication(next http.Handler) http.Handler {
	if !app.authEnabled() {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !app.isAuthenticated(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "请先完成密码登录"})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (app *application) isAuthenticated(r *http.Request) bool {
	if !app.authEnabled() {
		return true
	}

	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}

	return app.validateSessionToken(cookie.Value)
}

func (app *application) setSessionCookie(w http.ResponseWriter, r *http.Request) {
	expiresAt := time.Now().UTC().Add(sessionMaxAge)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    app.issueSessionToken(expiresAt),
		Path:     app.baseHref(),
		Expires:  expiresAt,
		MaxAge:   int(sessionMaxAge / time.Second),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsSecure(r),
	})
}

func (app *application) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     app.baseHref(),
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsSecure(r),
	})
}

func (app *application) issueSessionToken(expiresAt time.Time) string {
	expiryPart := strconv.FormatInt(expiresAt.UTC().Unix(), 10)
	mac := hmac.New(sha256.New, app.sessionSecret)
	_, _ = mac.Write([]byte(expiryPart))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(app.accessPassword))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return expiryPart + "." + signature
}

func (app *application) validateSessionToken(token string) bool {
	if token == "" || !strings.Contains(token, ".") {
		return false
	}

	expiryPart, signaturePart, ok := strings.Cut(token, ".")
	if !ok || expiryPart == "" || signaturePart == "" {
		return false
	}

	expiresAtUnix, err := strconv.ParseInt(expiryPart, 10, 64)
	if err != nil {
		return false
	}

	if time.Now().UTC().After(time.Unix(expiresAtUnix, 0).UTC()) {
		return false
	}

	expected := app.issueSessionToken(time.Unix(expiresAtUnix, 0).UTC())
	return subtle.ConstantTimeCompare([]byte(expected), []byte(token)) == 1
}

func loginClientKey(r *http.Request) string {
	if r == nil {
		return ""
	}

	if forwardedFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwardedFor != "" {
		if clientIP := strings.TrimSpace(strings.Split(forwardedFor, ",")[0]); clientIP != "" {
			return clientIP
		}
	}

	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}

	return strings.TrimSpace(r.RemoteAddr)
}

func writeLoginRateLimited(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := int(retryAfter.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}

	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{
		"error":             fmt.Sprintf("登录尝试过于频繁，请在 %d 秒后重试", seconds),
		"retryAfterSeconds": seconds,
	})
}

func parseListFilter(rawRating string, rawTags string) (store.ListFilter, error) {
	filter := store.ListFilter{}
	value := strings.TrimSpace(rawRating)
	if value == "" || value == "all" {
	} else {
		switch value {
		case "unrated":
			filter.Mode = "unrated"
		case "rated":
			filter.Mode = "rated"
		case "1", "2", "3", "4", "5":
			filter.Mode = "exact"
			filter.Rating = int(value[0] - '0')
		default:
			return store.ListFilter{}, fmt.Errorf("unsupported rating filter %q", value)
		}
	}

	tagValue := strings.TrimSpace(rawTags)
	if tagValue == "" {
		return filter, nil
	}

	for _, part := range strings.Split(tagValue, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		tagID, err := strconv.ParseInt(part, 10, 64)
		if err != nil || tagID <= 0 {
			return store.ListFilter{}, fmt.Errorf("invalid tag id %q", part)
		}
		filter.TagIDs = append(filter.TagIDs, tagID)
	}

	return filter, nil
}

func (app *application) backfillLocalCover(ctx context.Context, record store.VideoRecord, freshSourceURL string) (store.VideoRecord, error) {
	record.CoverSourceURL = firstNonEmpty(freshSourceURL, record.CoverSourceURL, record.CoverURL)
	if record.CoverSourceURL == "" {
		return record, nil
	}

	if record.CoverLocalFile != "" {
		if _, err := os.Stat(filepath.Join(app.rootDir, filepath.FromSlash(record.CoverLocalFile))); err == nil {
			record.CoverURL = firstNonEmpty(record.CoverLocalURL, record.CoverURL, record.CoverSourceURL)
			return record, nil
		}
	}

	coverLocalFile, coverLocalURL, coverSourceURL, err := app.storeCoverLocally(ctx, record.VideoID, record.CoverSourceURL)
	if err != nil {
		return record, err
	}

	record.CoverSourceURL = coverSourceURL
	record.CoverLocalFile = coverLocalFile
	record.CoverLocalURL = coverLocalURL
	record.CoverURL = firstNonEmpty(coverLocalURL, coverSourceURL, record.CoverURL)

	return app.store.Upsert(record)
}

func mergeRecordWithExtractedMetadata(record store.VideoRecord, extracted *downloader.ExtractedVideo, sourceInput string) store.VideoRecord {
	if extracted == nil {
		return record
	}

	record.ID = firstNonEmpty(record.ID, record.VideoID, extracted.VideoID)
	record.VideoID = firstNonEmpty(record.VideoID, extracted.VideoID)
	record.Title = firstNonEmpty(extracted.Title, record.Title)
	record.Description = firstNonEmpty(extracted.Description, record.Description)
	record.Author = firstNonEmpty(extracted.Author, record.Author)
	record.AuthorID = firstNonEmpty(extracted.AuthorID, record.AuthorID)
	record.ShareURL = firstNonEmpty(extracted.ShareURL, record.ShareURL)
	record.OriginalURL = firstNonEmpty(extracted.OriginalURL, record.OriginalURL)
	record.ContentType = firstNonEmpty(extracted.ContentType, record.ContentType)
	record.CoverSourceURL = firstNonEmpty(extracted.CoverURL, record.CoverSourceURL)
	record.CoverURL = firstNonEmpty(record.CoverLocalURL, record.CoverURL, record.CoverSourceURL, extracted.CoverURL)
	record.VideoURI = firstNonEmpty(extracted.VideoURI, record.VideoURI)
	record.DownloadURL = firstNonEmpty(extracted.VideoDownloadURL, record.DownloadURL)
	record.WatermarkURL = firstNonEmpty(extracted.WatermarkVideoURL, record.WatermarkURL)
	record.VideoWidth = preferPositiveInt64(extracted.VideoWidth, record.VideoWidth)
	record.VideoHeight = preferPositiveInt64(extracted.VideoHeight, record.VideoHeight)
	record.Duration = preferPositiveFloat64(extracted.Duration, record.Duration)
	record.LikeCount = preferPositiveInt64(extracted.LikeCount, record.LikeCount)
	record.CommentCount = preferPositiveInt64(extracted.CommentCount, record.CommentCount)
	record.ShareCount = preferPositiveInt64(extracted.ShareCount, record.ShareCount)
	record.CollectCount = preferPositiveInt64(extracted.CollectCount, record.CollectCount)

	if trimmed := strings.TrimSpace(sourceInput); trimmed != "" {
		record.LastSourceInput = trimmed
	}

	return record
}

func (app *application) storeCoverLocally(ctx context.Context, videoID, sourceURL string) (string, string, string, error) {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return "", "", "", nil
	}

	extension := coverFileExtension(sourceURL)
	fileName := videoID + extension
	relativeFile := filepath.ToSlash(filepath.Join(coverDataDir, fileName))
	absoluteFile := filepath.Join(app.rootDir, coverDataDir, fileName)
	localURL := "/covers/" + fileName
	localURL = app.routePath(localURL)

	if _, err := os.Stat(absoluteFile); err == nil {
		return relativeFile, localURL, sourceURL, nil
	}

	if _, err := downloader.DownloadToFile(ctx, sourceURL, absoluteFile); err != nil {
		return "", "", sourceURL, err
	}

	return relativeFile, localURL, sourceURL, nil
}

func coverFileExtension(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ".jpg"
	}

	extension := strings.ToLower(path.Ext(parsed.Path))
	switch extension {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".avif":
		return extension
	default:
		return ".jpg"
	}
}

func (app *application) serveFrontendHTML(w http.ResponseWriter, filePath string, pageBaseHref string) {
	indexHTML, err := fs.ReadFile(app.frontendFS, filePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	content := string(indexHTML)
	content = strings.Replace(content, "<head>", "<head>\n    <base href=\""+pageBaseHref+"\" />", 1)
	content = strings.ReplaceAll(content, "__APP_BASE_PATH__", app.basePath)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(content))
}

func (app *application) presentVideos(records []store.VideoRecord) []store.VideoRecord {
	if len(records) == 0 {
		return records
	}

	presented := make([]store.VideoRecord, 0, len(records))
	for _, record := range records {
		presented = append(presented, app.presentVideo(record))
	}

	return presented
}

func (app *application) presentVideo(record store.VideoRecord) store.VideoRecord {
	if record.LocalFile != "" {
		record.LocalURL = app.routePath("/media/" + path.Base(filepath.ToSlash(record.LocalFile)))
	}

	if record.CoverLocalFile != "" {
		record.CoverLocalURL = app.routePath("/covers/" + path.Base(filepath.ToSlash(record.CoverLocalFile)))
	}

	if record.CoverLocalURL != "" {
		record.CoverURL = record.CoverLocalURL
	}

	return record
}

func (app *application) baseHref() string {
	if app.basePath == "" {
		return "/"
	}
	return app.basePath + "/"
}

func (app *application) routePath(value string) string {
	return joinBasePath(app.basePath, value)
}

func (app *application) stripBasePath(value string) string {
	if app.basePath == "" {
		return value
	}
	if value == app.basePath {
		return "/"
	}
	if strings.HasPrefix(value, app.basePath+"/") {
		return strings.TrimPrefix(value, app.basePath)
	}
	return value
}

func decodeJSONBody(r *http.Request, destination any, maxBytes int64) error {
	r.Body = http.MaxBytesReader(responseWriterDiscard{}, r.Body, maxBytes)
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(destination); err != nil {
		return err
	}

	if decoder.More() {
		return errors.New("request body must contain a single JSON object")
	}

	return nil
}

func withJSONLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(startedAt).Truncate(time.Millisecond))
	})
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write(data)
}

func pathClean(path string) string {
	cleaned := filepath.ToSlash(filepath.Clean(path))
	if cleaned == "." {
		return "/"
	}
	if !strings.HasPrefix(cleaned, "/") {
		return "/" + cleaned
	}
	return cleaned
}

func normalizeBasePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "/" {
		return ""
	}

	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}

	value = pathClean(value)
	if value == "/" {
		return ""
	}

	return strings.TrimRight(value, "/")
}

func joinBasePath(basePath, value string) string {
	hadTrailingSlash := strings.HasSuffix(value, "/")

	if value == "" {
		value = "/"
	}

	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}

	value = pathClean(value)
	if basePath == "" {
		if hadTrailingSlash && value != "/" && !strings.HasSuffix(value, "/") {
			return value + "/"
		}
		return value
	}
	if value == "/" {
		return basePath + "/"
	}

	result := basePath + value
	if hadTrailingSlash && !strings.HasSuffix(result, "/") {
		result += "/"
	}

	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}

	return ""
}

func preferPositiveInt64(primary, fallback int64) int64 {
	if primary > 0 {
		return primary
	}

	return fallback
}

func preferPositiveFloat64(primary, fallback float64) float64 {
	if primary > 0 {
		return primary
	}

	return fallback
}

func generateSessionSecret() ([]byte, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}

	return secret, nil
}

func requestIsSecure(r *http.Request) bool {
	if r == nil {
		return false
	}

	if r.TLS != nil {
		return true
	}

	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

type responseWriterDiscard struct{}

func (responseWriterDiscard) Header() http.Header        { return http.Header{} }
func (responseWriterDiscard) Write([]byte) (int, error)  { return 0, nil }
func (responseWriterDiscard) WriteHeader(statusCode int) {}
