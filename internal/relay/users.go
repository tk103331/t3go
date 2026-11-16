package relay

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	userFileVersion  = 1
	userSessionTTL   = 30 * 24 * time.Hour
	maxUserSessions  = 20
	loginFailureTTL  = 5 * time.Minute
	maxLoginFailures = 5
)

// User is the public account shape returned by Relay authentication endpoints.
type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Provider  string    `json:"provider"`
	CreatedAt time.Time `json:"createdAt"`
}

// UserSession contains the opaque bearer token issued after registration or login.
// Relay stores only its digest, never the plaintext token.
type UserSession struct {
	User      User      `json:"user"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// UserCredentials is used by local and future OAuth account entry points.
type UserCredentials struct {
	Email    string `json:"email"`
	Password string `json:"password,omitempty"`
	Name     string `json:"name,omitempty"`
}

type userRecord struct {
	ID              string    `json:"id"`
	Email           string    `json:"email"`
	Name            string    `json:"name"`
	Provider        string    `json:"provider"`
	ProviderSubject string    `json:"providerSubject,omitempty"`
	PasswordHash    string    `json:"passwordHash,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type sessionRecord struct {
	UserID    string    `json:"userId"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

type authFailure struct {
	Count   int
	ResetAt time.Time
}

type userFile struct {
	Version  int                      `json:"version"`
	Users    map[string]userRecord    `json:"users"`
	Sessions map[string]sessionRecord `json:"sessions"`
}

func loadUserFile(path string) (userFile, error) {
	out := userFile{Version: userFileVersion, Users: map[string]userRecord{}, Sessions: map[string]sessionRecord{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Version != userFileVersion {
		return out, errors.New("invalid relay users file")
	}
	if out.Users == nil {
		out.Users = map[string]userRecord{}
	}
	if out.Sessions == nil {
		out.Sessions = map[string]sessionRecord{}
	}
	return out, nil
}

func savePrivateJSON(path, pattern string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), pattern)
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func normalizeEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func validEmail(email string) bool {
	if email == "" || len(email) > 254 || strings.ContainsAny(email, "\r\n") {
		return false
	}
	address, err := mail.ParseAddress(email)
	return err == nil && strings.EqualFold(address.Address, email)
}

func userFromRecord(record userRecord) User {
	return User{ID: record.ID, Email: record.Email, Name: record.Name, Provider: record.Provider, CreatedAt: record.CreatedAt}
}

func randomUserToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "t3go_user_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func newDummyPasswordHash() ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte("relay-invalid-password"), bcrypt.DefaultCost)
}

func randomUserID() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "usr_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func (h *Hub) saveUsersLocked() error {
	return savePrivateJSON(h.usersPath, ".relay-users-*", userFile{
		Version: userFileVersion, Users: h.users, Sessions: h.sessions,
	})
}

func (h *Hub) issueSessionLocked(userID string) (string, time.Time, error) {
	token, err := randomUserToken()
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now().UTC()
	expires := now.Add(userSessionTTL)
	for key, session := range h.sessions {
		if !session.ExpiresAt.After(now) {
			delete(h.sessions, key)
		}
	}
	userSessions := 0
	for _, session := range h.sessions {
		if session.UserID == userID {
			userSessions++
		}
	}
	if userSessions >= maxUserSessions {
		var oldestKey string
		var oldest time.Time
		for key, session := range h.sessions {
			if session.UserID != userID {
				continue
			}
			if oldest.IsZero() || session.CreatedAt.Before(oldest) {
				oldestKey, oldest = key, session.CreatedAt
			}
		}
		delete(h.sessions, oldestKey)
	}
	h.sessions[digest(token)] = sessionRecord{UserID: userID, ExpiresAt: expires, CreatedAt: now}
	return token, expires, nil
}

func loginFailureKey(r *http.Request, email string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return strings.ToLower(strings.TrimSpace(host)) + "|" + email
}

func (h *Hub) loginLimited(key string, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	failure, exists := h.authFailures[key]
	if !exists {
		return false
	}
	if !failure.ResetAt.After(now) {
		delete(h.authFailures, key)
		return false
	}
	return failure.Count >= maxLoginFailures
}

func (h *Hub) recordLoginFailure(key string, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	failure := h.authFailures[key]
	if !failure.ResetAt.After(now) {
		failure = authFailure{ResetAt: now.Add(loginFailureTTL)}
	}
	failure.Count++
	h.authFailures[key] = failure
}

func (h *Hub) clearLoginFailures(key string) {
	h.mu.Lock()
	delete(h.authFailures, key)
	h.mu.Unlock()
}

func (h *Hub) registrationAuth(r *http.Request) (userID string, admin, ok bool) {
	secret := bearer(r)
	if matches(h.admin, secret) {
		return "", true, true
	}
	if secret == "" {
		return "", false, false
	}
	h.mu.Lock()
	session, exists := h.sessions[digest(secret)]
	if exists && session.ExpiresAt.After(time.Now()) {
		if _, userExists := h.users[session.UserID]; userExists {
			h.mu.Unlock()
			return session.UserID, false, true
		}
	}
	h.mu.Unlock()
	return "", false, false
}

// validUserSessionLocked checks an ordinary account token. h.mu must be held.
func (h *Hub) validUserSessionLocked(secret string) bool {
	if secret == "" || matches(h.admin, secret) {
		return false
	}
	session, exists := h.sessions[digest(secret)]
	if !exists || !session.ExpiresAt.After(time.Now()) {
		return false
	}
	_, userExists := h.users[session.UserID]
	return userExists
}

func (h *Hub) registerUser(w http.ResponseWriter, r *http.Request) {
	var in UserCredentials
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, "registration", "invalid account registration")
		return
	}
	email := normalizeEmail(in.Email)
	password := in.Password
	name := strings.TrimSpace(in.Name)
	if !validEmail(email) {
		fail(w, http.StatusBadRequest, "email", "enter a valid email address")
		return
	}
	if len(password) < 8 || len(password) > 72 {
		fail(w, http.StatusBadRequest, "password", "password must be between 8 and 72 bytes")
		return
	}
	if len(name) > 80 {
		fail(w, http.StatusBadRequest, "name", "display name is too long")
		return
	}
	if name == "" {
		name = strings.SplitN(email, "@", 2)[0]
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fail(w, http.StatusInternalServerError, "password", "could not secure password")
		return
	}
	userID, err := randomUserID()
	if err != nil {
		fail(w, http.StatusInternalServerError, "account", "could not create account")
		return
	}
	now := time.Now().UTC()
	record := userRecord{ID: userID, Email: email, Name: name, Provider: "local",
		PasswordHash: string(hash), CreatedAt: now, UpdatedAt: now}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		fail(w, http.StatusServiceUnavailable, "closed", "relay is shutting down")
		return
	}
	for _, existing := range h.users {
		if strings.EqualFold(existing.Email, email) {
			h.mu.Unlock()
			fail(w, http.StatusConflict, "exists", "an account with this email already exists")
			return
		}
	}
	h.users[userID] = record
	token, expires, err := h.issueSessionLocked(userID)
	if err == nil {
		err = h.saveUsersLocked()
	}
	if err != nil {
		delete(h.users, userID)
		delete(h.sessions, digest(token))
		h.mu.Unlock()
		fail(w, http.StatusInternalServerError, "store", "could not save account")
		return
	}
	h.mu.Unlock()
	reply(w, http.StatusCreated, UserSession{User: userFromRecord(record), Token: token, ExpiresAt: expires})
}

func (h *Hub) loginUser(w http.ResponseWriter, r *http.Request) {
	var in UserCredentials
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, "login", "invalid account login")
		return
	}
	email := normalizeEmail(in.Email)
	failureKey := loginFailureKey(r, email)
	now := time.Now()
	if h.loginLimited(failureKey, now) {
		fail(w, http.StatusTooManyRequests, "rate_limited", "too many failed sign-in attempts; try again in a few minutes")
		return
	}
	h.mu.Lock()
	var record userRecord
	found := false
	for _, candidate := range h.users {
		if strings.EqualFold(candidate.Email, email) {
			record, found = candidate, true
			break
		}
	}
	h.mu.Unlock()
	hash := h.dummyPasswordHash
	if found && record.Provider == "local" && record.PasswordHash != "" {
		hash = []byte(record.PasswordHash)
	}
	passwordOK := bcrypt.CompareHashAndPassword(hash, []byte(in.Password)) == nil
	if !found || !passwordOK || record.Provider != "local" {
		h.recordLoginFailure(failureKey, now)
		fail(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
		return
	}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		fail(w, http.StatusServiceUnavailable, "closed", "relay is shutting down")
		return
	}
	current, exists := h.users[record.ID]
	if !exists || subtle.ConstantTimeCompare([]byte(current.PasswordHash), []byte(record.PasswordHash)) != 1 {
		h.mu.Unlock()
		h.recordLoginFailure(failureKey, now)
		fail(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
		return
	}
	token, expires, err := h.issueSessionLocked(record.ID)
	if err == nil {
		err = h.saveUsersLocked()
	}
	if err != nil {
		delete(h.sessions, digest(token))
		h.mu.Unlock()
		fail(w, http.StatusInternalServerError, "store", "could not save login session")
		return
	}
	h.mu.Unlock()
	h.clearLoginFailures(failureKey)
	reply(w, http.StatusOK, UserSession{User: userFromRecord(current), Token: token, ExpiresAt: expires})
}

func (h *Hub) currentUser(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	session, exists := h.sessions[digest(bearer(r))]
	record, userExists := h.users[session.UserID]
	valid := exists && userExists && session.ExpiresAt.After(time.Now())
	h.mu.Unlock()
	if !valid {
		fail(w, http.StatusUnauthorized, "unauthorized", "user token is invalid or expired")
		return
	}
	reply(w, http.StatusOK, map[string]any{"user": userFromRecord(record)})
}

func (h *Hub) logoutUser(w http.ResponseWriter, r *http.Request) {
	key := digest(bearer(r))
	h.mu.Lock()
	if _, exists := h.sessions[key]; !exists {
		h.mu.Unlock()
		fail(w, http.StatusUnauthorized, "unauthorized", "user token is invalid or expired")
		return
	}
	delete(h.sessions, key)
	err := h.saveUsersLocked()
	h.mu.Unlock()
	if err != nil {
		fail(w, http.StatusInternalServerError, "store", "could not revoke login session")
		return
	}
	reply(w, http.StatusOK, map[string]bool{"revoked": true})
}
