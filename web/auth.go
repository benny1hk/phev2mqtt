package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	sessionCookieName = "phev_session"
	sessionTTL        = 24 * time.Hour
)

type session struct {
	username  string
	expiresAt time.Time
}

type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*session
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: map[string]*session{}}
}

func (s *sessionStore) create(username string) (string, error) {
	id, err := randomID()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.sessions[id] = &session{username: username, expiresAt: time.Now().Add(sessionTTL)}
	s.mu.Unlock()
	return id, nil
}

func (s *sessionStore) get(id string) (string, bool) {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return "", false
	}
	if time.Now().After(sess.expiresAt) {
		s.delete(id)
		return "", false
	}
	// Sliding expiry: refresh on each successful lookup.
	s.mu.Lock()
	sess.expiresAt = time.Now().Add(sessionTTL)
	s.mu.Unlock()
	return sess.username, true
}

func (s *sessionStore) delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

// deleteAllExcept invalidates every session, optionally keeping one. Used
// after a password change so old browser sessions are forced to re-auth.
func (s *sessionStore) deleteAllExcept(keepID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.sessions {
		if id != keepID {
			delete(s.sessions, id)
		}
	}
}

func randomID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ctxKeySession is the type used to stash the session ID on a request
// context. Defining a dedicated type avoids accidental key collisions.
type ctxKey int

const (
	ctxKeySessionID ctxKey = iota
)

func sessionIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeySessionID).(string)
	return v
}

// requireAuth wraps a handler so that callers must present a valid session
// cookie. Failures produce a 401 with a JSON error body.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			writeError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		if _, ok := s.sessions.get(cookie.Value); !ok {
			writeError(w, http.StatusUnauthorized, "session expired")
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeySessionID, cookie.Value)
		next(w, r.WithContext(ctx))
	}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !s.creds.verify(req.Username, req.Password) {
		log.Warnf("web login failed for username %q from %s", req.Username, r.RemoteAddr)
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	id, err := s.sessions.create(req.Username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		s.sessions.delete(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	username, _, must := s.creds.get()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"username":             username,
		"must_change_password": must,
	})
}

type passwordRequest struct {
	Current string `json:"current"`
	New     string `json:"new"`
}

func (s *Server) handlePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req passwordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	username, _, _ := s.creds.get()
	if !s.creds.verify(username, req.Current) {
		writeError(w, http.StatusForbidden, "current password is incorrect")
		return
	}
	if len(req.New) < 8 {
		writeError(w, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}
	if err := s.creds.setPassword(req.New); err != nil {
		log.Errorf("password change failed: %v", err)
		writeError(w, http.StatusInternalServerError, "could not update password")
		return
	}
	s.sessions.deleteAllExcept(sessionIDFromContext(r.Context()))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
