package main

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	sessionTTL       = 7 * 24 * time.Hour
	pbkdf2Iterations = 120000
	passwordKeyBytes = 32
	maxBodyBytes     = 1 << 20
)

type Server struct {
	httpClient *http.Client
	store      *Store
}

type Store struct {
	mu             sync.RWMutex
	path           string
	sessions       map[string]*Session
	userIDsByEmail map[string]string
	usersByID      map[string]*User
}

type Session struct {
	ExpiresAt time.Time `json:"expiresAt"`
	Token     string    `json:"token"`
	UserID    string    `json:"userId"`
}

type User struct {
	Bio          string `json:"bio"`
	CreatedAt    string `json:"createdAt"`
	Email        string `json:"email"`
	Goal         string `json:"goal"`
	ID           string `json:"id"`
	Location     string `json:"location"`
	Name         string `json:"name"`
	PasswordHash string `json:"passwordHash"`
	PasswordSalt string `json:"passwordSalt"`
	UpdatedAt    string `json:"updatedAt"`
	Workspace    string `json:"workspace"`
}

type persistedState struct {
	Users []*User `json:"users"`
}

type clientUser struct {
	Bio       string `json:"bio"`
	CreatedAt string `json:"createdAt"`
	Email     string `json:"email"`
	Goal      string `json:"goal"`
	ID        string `json:"id"`
	Location  string `json:"location"`
	Name      string `json:"name"`
	UpdatedAt string `json:"updatedAt"`
	Workspace string `json:"workspace"`
}

type sessionView struct {
	ExpiresAt string `json:"expiresAt"`
	Source    string `json:"source"`
}

type authResponse struct {
	Session sessionView `json:"session"`
	Token   string      `json:"token,omitempty"`
	User    clientUser  `json:"user"`
}

type messageResponse struct {
	Message string `json:"message"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type registerRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type updateProfileRequest struct {
	Bio       string `json:"bio"`
	Goal      string `json:"goal"`
	Location  string `json:"location"`
	Workspace string `json:"workspace"`
}

type catalogResponse struct {
	Discover     []discoverBook     `json:"discover"`
	Downloadable []downloadableBook `json:"downloadable"`
	Page         int                `json:"page"`
	Query        string             `json:"query"`
}

type downloadableBook struct {
	Authors        []string         `json:"authors"`
	CoverURL       string           `json:"coverUrl,omitempty"`
	DownloadCount  int              `json:"downloadCount"`
	Downloads      []downloadOption `json:"downloads"`
	ID             string           `json:"id"`
	IsPublicDomain bool             `json:"isPublicDomain"`
	Languages      []string         `json:"languages"`
	Source         string           `json:"source"`
	Subjects       []string         `json:"subjects"`
	Title          string           `json:"title"`
}

type discoverBook struct {
	Authors          []string `json:"authors"`
	CoverURL         string   `json:"coverUrl,omitempty"`
	EbookAccess      string   `json:"ebookAccess"`
	EditionCount     int      `json:"editionCount"`
	FirstPublishYear int      `json:"firstPublishYear"`
	ID               string   `json:"id"`
	OpenLibraryURL   string   `json:"openLibraryUrl"`
	Source           string   `json:"source"`
	Title            string   `json:"title"`
}

type downloadOption struct {
	Label string `json:"label"`
	Mime  string `json:"mime"`
	URL   string `json:"url"`
}

type downloadableSearchResult struct {
	Books []downloadableBook
	Err   error
}

type discoverSearchResult struct {
	Books []discoverBook
	Err   error
}

type gutendexResponse struct {
	Results []gutendexBook `json:"results"`
}

type gutendexBook struct {
	Authors       []gutendexAuthor  `json:"authors"`
	Copyright     bool              `json:"copyright"`
	DownloadCount int               `json:"download_count"`
	Formats       map[string]string `json:"formats"`
	ID            int               `json:"id"`
	Languages     []string          `json:"languages"`
	Subjects      []string          `json:"subjects"`
	Title         string            `json:"title"`
}

type gutendexAuthor struct {
	Name string `json:"name"`
}

type openLibrarySearchResponse struct {
	Docs []openLibraryDoc `json:"docs"`
}

type openLibraryDoc struct {
	AuthorNames      []string `json:"author_name"`
	CoverID          int      `json:"cover_i"`
	EditionCount     int      `json:"edition_count"`
	EbookAccess      string   `json:"ebook_access"`
	FirstPublishYear int      `json:"first_publish_year"`
	Key              string   `json:"key"`
	Title            string   `json:"title"`
}

func main() {
	baseDir, err := resolveBackendDir()
	if err != nil {
		log.Fatalf("resolve backend dir: %v", err)
	}

	store, err := NewStore(filepath.Join(baseDir, "data", "users.json"))
	if err != nil {
		log.Fatalf("init store: %v", err)
	}

	server := &Server{
		httpClient: newOutboundClient(),
		store:      store,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", server.handleHealth)
	mux.HandleFunc("POST /api/auth/register", server.handleRegister)
	mux.HandleFunc("POST /api/auth/login", server.handleLogin)
	mux.HandleFunc("GET /api/auth/me", server.handleMe)
	mux.HandleFunc("POST /api/auth/logout", server.handleLogout)
	mux.HandleFunc("PUT /api/profile", server.handleUpdateProfile)
	mux.HandleFunc("GET /api/books/search", server.handleSearchBooks)

	port := envOrDefault("PORT", "8080")
	addr := ":" + port

	log.Printf("Go API listening on %s", addr)
	log.Printf("Public-domain download source: https://gutendex.com/")
	log.Printf("Discovery metadata source: https://openlibrary.org/")

	if err := http.ListenAndServe(addr, withCORS(withLogging(mux))); err != nil {
		log.Fatalf("listen and serve: %v", err)
	}
}

func resolveBackendDir() (string, error) {
	workingDir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	candidates := []string{
		workingDir,
		filepath.Join(workingDir, "backend"),
		filepath.Join(workingDir, "..", "backend"),
	}

	for _, candidate := range candidates {
		if fileExists(filepath.Join(candidate, "go.mod")) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("could not find backend directory from %s", workingDir)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}

func newOutboundClient() *http.Client {
	dialer := &net.Dialer{
		KeepAlive: 30 * time.Second,
		Timeout:   8 * time.Second,
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", addr)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: 12 * time.Second,
		TLSHandshakeTimeout:   8 * time.Second,
	}

	return &http.Client{
		Timeout:   20 * time.Second,
		Transport: transport,
	}
}

func NewStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	store := &Store{
		path:           path,
		sessions:       map[string]*Session{},
		userIDsByEmail: map[string]string{},
		usersByID:      map[string]*User{},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}

		return nil, err
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return store, nil
	}

	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	for _, user := range state.Users {
		store.usersByID[user.ID] = user
		store.userIDsByEmail[strings.ToLower(strings.TrimSpace(user.Email))] = user.ID
	}

	return store, nil
}

func (s *Store) Register(name, email, password string) (*User, *Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleanName := strings.TrimSpace(name)
	cleanEmail := strings.ToLower(strings.TrimSpace(email))

	if _, exists := s.userIDsByEmail[cleanEmail]; exists {
		return nil, nil, fmt.Errorf("an account with that email already exists")
	}

	salt, err := randomBytes(16)
	if err != nil {
		return nil, nil, err
	}

	passwordHash, err := derivePasswordHash(password, salt)
	if err != nil {
		return nil, nil, err
	}

	userID, err := randomID("usr_")
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	user := &User{
		Bio:          fmt.Sprintf("%s is building a cleaner reading experience.", cleanName),
		CreatedAt:    now,
		Email:        cleanEmail,
		Goal:         "Search and download public-domain books",
		ID:           userID,
		Location:     "",
		Name:         cleanName,
		PasswordHash: passwordHash,
		PasswordSalt: base64.RawURLEncoding.EncodeToString(salt),
		UpdatedAt:    now,
		Workspace:    "Go API + library downloads",
	}

	s.usersByID[user.ID] = user
	s.userIDsByEmail[user.Email] = user.ID

	if err := s.saveLocked(); err != nil {
		delete(s.usersByID, user.ID)
		delete(s.userIDsByEmail, user.Email)
		return nil, nil, err
	}

	session, err := s.createSessionLocked(user.ID)
	if err != nil {
		return nil, nil, err
	}

	return cloneUser(user), cloneSession(session), nil
}

func (s *Store) Login(email, password string) (*User, *Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	userID, exists := s.userIDsByEmail[strings.ToLower(strings.TrimSpace(email))]
	if !exists {
		return nil, nil, fmt.Errorf("invalid email or password")
	}

	user := s.usersByID[userID]
	if !verifyPassword(password, user.PasswordSalt, user.PasswordHash) {
		return nil, nil, fmt.Errorf("invalid email or password")
	}

	session, err := s.createSessionLocked(user.ID)
	if err != nil {
		return nil, nil, err
	}

	return cloneUser(user), cloneSession(session), nil
}

func (s *Store) GetSessionUser(token string) (*Session, *User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[token]
	if !exists {
		return nil, nil, fmt.Errorf("session not found")
	}

	if time.Now().After(session.ExpiresAt) {
		delete(s.sessions, token)
		return nil, nil, fmt.Errorf("session expired")
	}

	user, exists := s.usersByID[session.UserID]
	if !exists {
		delete(s.sessions, token)
		return nil, nil, fmt.Errorf("user not found")
	}

	return cloneSession(session), cloneUser(user), nil
}

func (s *Store) DeleteSession(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, token)
}

func (s *Store) UpdateProfile(userID string, update updateProfileRequest) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, exists := s.usersByID[userID]
	if !exists {
		return nil, fmt.Errorf("user not found")
	}

	user.Bio = strings.TrimSpace(update.Bio)
	user.Goal = strings.TrimSpace(update.Goal)
	user.Location = strings.TrimSpace(update.Location)
	user.Workspace = strings.TrimSpace(update.Workspace)
	user.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := s.saveLocked(); err != nil {
		return nil, err
	}

	return cloneUser(user), nil
}

func (s *Store) createSessionLocked(userID string) (*Session, error) {
	token, err := randomID("tok_")
	if err != nil {
		return nil, err
	}

	session := &Session{
		ExpiresAt: time.Now().Add(sessionTTL),
		Token:     token,
		UserID:    userID,
	}

	s.sessions[token] = session

	return session, nil
}

func (s *Store) saveLocked() error {
	users := make([]*User, 0, len(s.usersByID))
	for _, user := range s.usersByID {
		users = append(users, cloneUser(user))
	}

	sort.Slice(users, func(i, j int) bool {
		return users[i].CreatedAt < users[j].CreatedAt
	})

	payload, err := json.MarshalIndent(persistedState{Users: users}, "", "  ")
	if err != nil {
		return err
	}

	tempPath := s.path + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0o600); err != nil {
		return err
	}

	return os.Rename(tempPath, s.path)
}

func cloneUser(user *User) *User {
	if user == nil {
		return nil
	}

	copy := *user
	return &copy
}

func cloneSession(session *Session) *Session {
	if session == nil {
		return nil
	}

	copy := *session
	return &copy
}

func derivePasswordHash(password string, salt []byte) (string, error) {
	derived, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, passwordKeyBytes)
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(derived), nil
}

func verifyPassword(password, saltEncoded, hashEncoded string) bool {
	salt, err := base64.RawURLEncoding.DecodeString(saltEncoded)
	if err != nil {
		return false
	}

	expected, err := base64.RawURLEncoding.DecodeString(hashEncoded)
	if err != nil {
		return false
	}

	derived, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, len(expected))
	if err != nil {
		return false
	}

	return subtle.ConstantTimeCompare(derived, expected) == 1
}

func randomBytes(length int) ([]byte, error) {
	output := make([]byte, length)
	if _, err := rand.Read(output); err != nil {
		return nil, err
	}

	return output, nil
}

func randomID(prefix string) (string, error) {
	random, err := randomBytes(24)
	if err != nil {
		return "", err
	}

	return prefix + base64.RawURLEncoding.EncodeToString(random), nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":    "native3-go-api",
		"status":  "ok",
		"sources": []string{"Gutendex", "Open Library"},
	})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var request registerRequest
	if err := readJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if len(strings.TrimSpace(request.Name)) < 2 {
		writeError(w, http.StatusBadRequest, "name must be at least 2 characters")
		return
	}

	if !strings.Contains(strings.TrimSpace(request.Email), "@") {
		writeError(w, http.StatusBadRequest, "enter a valid email address")
		return
	}

	if len(request.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	user, session, err := s.store.Register(request.Name, request.Email, request.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, authResponse{
		Session: toSessionView(session),
		Token:   session.Token,
		User:    toClientUser(user),
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if err := readJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	user, session, err := s.store.Login(request.Email, request.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, authResponse{
		Session: toSessionView(session),
		Token:   session.Token,
		User:    toClientUser(user),
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	session, user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, authResponse{
		Session: toSessionView(session),
		User:    toClientUser(user),
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}

	s.store.DeleteSession(token)
	writeJSON(w, http.StatusOK, messageResponse{Message: "signed out"})
}

func (s *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	_, user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}

	var request updateProfileRequest
	if err := readJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	updatedUser, err := s.store.UpdateProfile(user.ID, request)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]clientUser{
		"user": toClientUser(updatedUser),
	})
}

func (s *Server) handleSearchBooks(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	page := parsePositiveInt(r.URL.Query().Get("page"), 1)

	downloadCtx, cancelDownload := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancelDownload()

	discoverCtx, cancelDiscover := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancelDiscover()

	downloadCh := make(chan downloadableSearchResult, 1)
	discoverCh := make(chan discoverSearchResult, 1)

	go func() {
		books, err := s.searchGutendex(downloadCtx, query, page)
		downloadCh <- downloadableSearchResult{
			Books: books,
			Err:   err,
		}
	}()

	go func() {
		books, err := s.searchOpenLibrary(discoverCtx, query, page)
		discoverCh <- discoverSearchResult{
			Books: books,
			Err:   err,
		}
	}()

	downloadResult := <-downloadCh
	if downloadResult.Err != nil {
		log.Printf("gutendex search error: %v", downloadResult.Err)
		writeError(w, http.StatusBadGateway, "could not fetch downloadable books right now")
		return
	}

	downloadable := downloadResult.Books
	discover := []discoverBook{}

	select {
	case discoverResult := <-discoverCh:
		if discoverResult.Err != nil {
			log.Printf("open library search warning: %v", discoverResult.Err)
		} else if discoverResult.Books != nil {
			discover = discoverResult.Books
		}
	default:
		cancelDiscover()
	}

	if downloadable == nil {
		downloadable = []downloadableBook{}
	}

	if discover == nil {
		discover = []discoverBook{}
	}

	writeJSON(w, http.StatusOK, catalogResponse{
		Discover:     discover,
		Downloadable: downloadable,
		Page:         page,
		Query:        query,
	})
}

func (s *Server) searchGutendex(ctx context.Context, query string, page int) ([]downloadableBook, error) {
	endpoint, err := url.Parse("https://gutendex.com/books/")
	if err != nil {
		return nil, err
	}

	params := endpoint.Query()
	if query != "" {
		params.Set("search", query)
	}
	if page > 1 {
		params.Set("page", strconv.Itoa(page))
	}
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "native3-go-api/1.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("gutendex error: %s", strings.TrimSpace(string(body)))
	}

	var payload gutendexResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	books := make([]downloadableBook, 0, len(payload.Results))
	for _, item := range payload.Results {
		downloads := buildDownloadOptions(item.Formats)
		if len(downloads) == 0 {
			continue
		}

		books = append(books, downloadableBook{
			Authors:        collectGutendexAuthors(item.Authors),
			CoverURL:       item.Formats["image/jpeg"],
			DownloadCount:  item.DownloadCount,
			Downloads:      downloads,
			ID:             fmt.Sprintf("gutendex-%d", item.ID),
			IsPublicDomain: !item.Copyright,
			Languages:      item.Languages,
			Source:         "Gutendex / Project Gutenberg",
			Subjects:       limitStrings(item.Subjects, 4),
			Title:          item.Title,
		})
	}

	return books, nil
}

func (s *Server) searchOpenLibrary(ctx context.Context, query string, page int) ([]discoverBook, error) {
	if query == "" {
		return nil, nil
	}

	endpoint, err := url.Parse("https://openlibrary.org/search.json")
	if err != nil {
		return nil, err
	}

	params := endpoint.Query()
	params.Set("q", query)
	params.Set("page", strconv.Itoa(page))
	params.Set("limit", "8")
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "native3-go-api/1.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("open library error: %s", strings.TrimSpace(string(body)))
	}

	var payload openLibrarySearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	books := make([]discoverBook, 0, len(payload.Docs))
	for _, item := range payload.Docs {
		if item.Title == "" || item.Key == "" {
			continue
		}

		books = append(books, discoverBook{
			Authors:          item.AuthorNames,
			CoverURL:         buildOpenLibraryCoverURL(item.CoverID),
			EbookAccess:      item.EbookAccess,
			EditionCount:     item.EditionCount,
			FirstPublishYear: item.FirstPublishYear,
			ID:               strings.TrimPrefix(item.Key, "/works/"),
			OpenLibraryURL:   "https://openlibrary.org" + item.Key,
			Source:           "Open Library discovery",
			Title:            item.Title,
		})
	}

	return books, nil
}

func collectGutendexAuthors(authors []gutendexAuthor) []string {
	output := make([]string, 0, len(authors))
	for _, author := range authors {
		if author.Name != "" {
			output = append(output, author.Name)
		}
	}

	if len(output) == 0 {
		return []string{"Unknown author"}
	}

	return output
}

func buildDownloadOptions(formats map[string]string) []downloadOption {
	type formatPreference struct {
		Label  string
		Prefix string
	}

	preferences := []formatPreference{
		{Label: "EPUB", Prefix: "application/epub+zip"},
		{Label: "MOBI", Prefix: "application/x-mobipocket-ebook"},
		{Label: "PDF", Prefix: "application/pdf"},
		{Label: "HTML", Prefix: "text/html"},
		{Label: "Plain Text", Prefix: "text/plain"},
	}

	options := make([]downloadOption, 0, len(preferences))
	for _, preference := range preferences {
		for mime, rawURL := range formats {
			if !strings.HasPrefix(mime, preference.Prefix) {
				continue
			}
			if !strings.HasPrefix(rawURL, "http") {
				continue
			}

			options = append(options, downloadOption{
				Label: preference.Label,
				Mime:  mime,
				URL:   rawURL,
			})
			break
		}
	}

	return options
}

func buildOpenLibraryCoverURL(coverID int) string {
	if coverID <= 0 {
		return ""
	}

	return fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-M.jpg", coverID)
}

func limitStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}

	return values[:limit]
}

func parsePositiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}

	return parsed
}

func toClientUser(user *User) clientUser {
	return clientUser{
		Bio:       user.Bio,
		CreatedAt: user.CreatedAt,
		Email:     user.Email,
		Goal:      user.Goal,
		ID:        user.ID,
		Location:  user.Location,
		Name:      user.Name,
		UpdatedAt: user.UpdatedAt,
		Workspace: user.Workspace,
	}
}

func toSessionView(session *Session) sessionView {
	return sessionView{
		ExpiresAt: session.ExpiresAt.UTC().Format(time.RFC3339),
		Source:    "Go API",
	}
}

func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request) (*Session, *User, bool) {
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return nil, nil, false
	}

	session, user, err := s.store.GetSessionUser(token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return nil, nil, false
	}

	return session, user, true
}

func bearerToken(header string) string {
	if header == "" {
		return ""
	}

	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}

	return strings.TrimSpace(parts[1])
}

func readJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid JSON payload")
	}

	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
