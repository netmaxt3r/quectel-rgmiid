package web

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const sessionCookieName = "rgmii_session"
const sessionDuration = 24 * time.Hour

func generateSessionID() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func checkSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}

// authenticate checks if the credentials are valid.
func (s *Server) authenticate(username, password string) bool {
	if s.authUser == "" || s.authPass == "" {
		return false
	}

	userHash := sha256.Sum256([]byte(username))
	passHash := sha256.Sum256([]byte(password))
	authUserHash := sha256.Sum256([]byte(s.authUser))
	authPassHash := sha256.Sum256([]byte(s.authPass))

	userMatch := subtle.ConstantTimeCompare(userHash[:], authUserHash[:]) == 1
	passMatch := subtle.ConstantTimeCompare(passHash[:], authPassHash[:]) == 1
	return userMatch && passMatch
}

// createSession generates a session ID, stores it, and sets the session cookie.
func (s *Server) createSession(w http.ResponseWriter) (string, error) {
	sessionID, err := generateSessionID()
	if err != nil {
		return "", err
	}

	s.sessMutex.Lock()
	s.sessions[sessionID] = time.Now().Add(sessionDuration)
	s.sessMutex.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		Expires:  time.Now().Add(sessionDuration),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	return sessionID, nil
}

// destroySession removes the session and clears the session cookie.
func (s *Server) destroySession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		s.sessMutex.Lock()
		delete(s.sessions, cookie.Value)
		s.sessMutex.Unlock()
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) checkAPIKey(r *http.Request) bool {
	if s.apiKey == "" {
		return false
	}

	var token string
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token = strings.TrimPrefix(authHeader, "Bearer ")
	} else {
		token = r.Header.Get("X-API-Key")
	}

	if token == "" {
		return false
	}

	tokenHash := sha256.Sum256([]byte(token))
	configHash := sha256.Sum256([]byte(s.apiKey))
	return subtle.ConstantTimeCompare(tokenHash[:], configHash[:]) == 1
}

func (s *Server) isSessionValid(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}

	s.sessMutex.RLock()
	expiry, exists := s.sessions[cookie.Value]
	s.sessMutex.RUnlock()

	if !exists {
		return false
	}

	if time.Now().After(expiry) {
		s.sessMutex.Lock()
		delete(s.sessions, cookie.Value)
		s.sessMutex.Unlock()
		return false
	}

	return true
}

func (s *Server) sessionOrTokenAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.authUser == "" || s.authPass == "" {
			next.ServeHTTP(w, r)
			return
		}

		if s.checkAPIKey(r) {
			next.ServeHTTP(w, r)
			return
		}

		if s.isSessionValid(r) {
			next.ServeHTTP(w, r)
			return
		}

		acceptHeader := r.Header.Get("Accept")
		isHTML := strings.Contains(acceptHeader, "text/html") || r.URL.Path == "/"
		isHX := r.Header.Get("HX-Request") == "true"

		if isHX {
			w.Header().Set("HX-Redirect", "/login")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if isHTML {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

func (s *Server) csrfProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isHX := r.Header.Get("HX-Request") == "true"
		if r.Method == http.MethodPost && isHX {
			if !checkSameOrigin(r) {
				http.Error(w, "Forbidden (potential CSRF)", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
