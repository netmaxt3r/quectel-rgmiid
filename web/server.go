package web

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"rgmii/daemon"
)

//go:embed templates/* static/*
var webFS embed.FS

var (
	tmpl = template.Must(template.ParseFS(webFS, "templates/index.html", "templates/status.html", "templates/login.html"))
)

// Server coordinates routing HTTP requests.
type Server struct {
	daemon    *daemon.Daemon
	modemAddr string
	authUser  string
	authPass  string
	apiKey    string
	sessions  map[string]time.Time
	sessMutex sync.RWMutex
}

// NewServer creates a new HTTP dashboard server.
func NewServer(daemon *daemon.Daemon, modemAddr, authUser, authPass, apiKey string) *Server {
	return &Server{
		daemon:    daemon,
		modemAddr: modemAddr,
		authUser:  authUser,
		authPass:  authPass,
		apiKey:    apiKey,
		sessions:  make(map[string]time.Time),
	}
}

// Start registers handlers and binds to the specified port.
func (s *Server) Start(port string) error {
	mux := http.NewServeMux()
	s.routes(mux)
	log.Printf("Starting web control panel on http://localhost:%s", port)
	
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return server.ListenAndServe()
}

func (s *Server) routes(mux *http.ServeMux) {
	mux.Handle("GET /{$}", s.sessionOrTokenAuth(http.HandlerFunc(s.handleIndex)))
	mux.Handle("GET /api/status", s.sessionOrTokenAuth(http.HandlerFunc(s.handleStatus)))
	mux.Handle("GET /api/refresh", s.sessionOrTokenAuth(http.HandlerFunc(s.handleRefresh)))
	mux.Handle("POST /api/cmd", s.sessionOrTokenAuth(s.csrfProtect(http.HandlerFunc(s.handleCmd))))
	mux.Handle("POST /api/cmd/json", s.sessionOrTokenAuth(s.csrfProtect(http.HandlerFunc(s.handleCmdJSON))))
	if os.Getenv("QUECTEL_DEBUG") == "1" {
		mux.Handle("GET /api/debug", s.sessionOrTokenAuth(http.HandlerFunc(s.handleDebug)))
	}
	mux.Handle("POST /api/modem/restart", s.sessionOrTokenAuth(s.csrfProtect(http.HandlerFunc(s.handleModemRestart))))
	mux.Handle("POST /api/sms/delete", s.sessionOrTokenAuth(s.csrfProtect(http.HandlerFunc(s.handleDeleteSMS))))

	mux.HandleFunc("GET /login", s.handleLoginGet)
	mux.HandleFunc("POST /login", s.handleLoginPost)
	mux.HandleFunc("GET /logout", s.handleLogout)

	// Serve static files with gzip compression - EXEMPTED from auth!
	mux.Handle("GET /static/", gzipHandler(http.FileServer(http.FS(webFS))))
}

func (s *Server) csrfProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if !checkSameOrigin(r) {
				http.Error(w, "Forbidden (potential CSRF)", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
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

func (s *Server) renderTemplate(w http.ResponseWriter, status int, name string, data interface{}) {
	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, name, data)
	if err != nil {
		log.Printf("Template render error (%s): %v", name, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := buf.WriteTo(w); err != nil {
		log.Printf("Socket write error (%s): %v", name, err)
	}
}

func (s *Server) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	if s.authUser == "" || s.authPass == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	if s.isSessionValid(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	s.renderTemplate(w, http.StatusOK, "login.html", nil)
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	if s.authUser == "" || s.authPass == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	err := r.ParseForm()
	if err != nil {
		s.renderTemplate(w, http.StatusOK, "login.html", map[string]string{"Error": "Invalid form submission"})
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	userHash := sha256.Sum256([]byte(username))
	passHash := sha256.Sum256([]byte(password))
	authUserHash := sha256.Sum256([]byte(s.authUser))
	authPassHash := sha256.Sum256([]byte(s.authPass))

	userMatch := subtle.ConstantTimeCompare(userHash[:], authUserHash[:]) == 1
	passMatch := subtle.ConstantTimeCompare(passHash[:], authPassHash[:]) == 1

	if !userMatch || !passMatch {
		s.renderTemplate(w, http.StatusUnauthorized, "login.html", map[string]string{"Error": "Invalid username or password"})
		return
	}

	sessionID, err := generateSessionID()
	if err != nil {
		s.renderTemplate(w, http.StatusInternalServerError, "login.html", map[string]string{"Error": "Internal server error"})
		return
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

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
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

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"AuthEnabled": s.authUser != "" && s.authPass != "",
	}
	s.renderTemplate(w, http.StatusOK, "index.html", data)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := s.daemon.GetStatus()
	
	// Inject modem address for client script consumption
	status.RawResponses["modem_addr"] = s.modemAddr

	s.renderTemplate(w, http.StatusOK, "status.html", status)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	s.daemon.PollAll()
	s.handleStatus(w, r)
}

func (s *Server) handleCmd(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	cmd := r.FormValue("cmd")
	if cmd == "" {
		http.Error(w, "Command is empty", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<div class=\"mb-3\"><span class=\"text-emerald-400\">&gt; %s</span><br>", template.HTMLEscapeString(cmd))

	resp, err := s.daemon.SendCommand(cmd)
	if err != nil {
		fmt.Fprintf(w, "<span class=\"text-rose-500 font-medium\">Error: %s</span></div>", template.HTMLEscapeString(err.Error()))
		return
	}

	// Escape response and map linebreaks for layout rendering
	escapedResp := template.HTMLEscapeString(resp)
	escapedResp = strings.ReplaceAll(escapedResp, "\n", "<br>")
	escapedResp = strings.ReplaceAll(escapedResp, "\r", "")
	fmt.Fprintf(w, "<span class=\"text-slate-300 font-mono text-xs leading-relaxed\">%s</span></div>", escapedResp)
}

type cmdRequest struct {
	Cmd string `json:"cmd"`
}

type cmdResponse struct {
	Success  bool   `json:"success"`
	Response string `json:"response,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (s *Server) handleCmdJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(cmdResponse{Success: false, Error: "Content-Type must be application/json"})
		return
	}

	var req cmdRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(cmdResponse{Success: false, Error: "Invalid JSON request"})
		return
	}

	if req.Cmd == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(cmdResponse{Success: false, Error: "Command is empty"})
		return
	}

	resp, err := s.daemon.SendCommand(req.Cmd)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(cmdResponse{Success: false, Error: err.Error()})
		return
	}

	json.NewEncoder(w).Encode(cmdResponse{Success: true, Response: resp})
}

func (s *Server) handleDebug(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := s.daemon.GetStatus()
	status.RawResponses["modem_addr"] = s.modemAddr
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(status)
}

func (s *Server) handleModemRestart(w http.ResponseWriter, r *http.Request) {
	log.Println("Received request to restart modem. Sending AT+CFUN=1,1...")
	_, err := s.daemon.SendCommand("AT+CFUN=1,1")
	if err != nil {
		http.Error(w, fmt.Sprintf("Restart failed: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte("<span class=\"text-emerald-400 font-medium\">Modem restart command (AT+CFUN=1,1) executed successfully. Modem is rebooting.</span>"))
}

func (s *Server) handleDeleteSMS(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	indexStr := r.FormValue("index")
	if indexStr == "" {
		http.Error(w, "Index is required", http.StatusBadRequest)
		return
	}

	var index int
	_, err = fmt.Sscanf(indexStr, "%d", &index)
	if err != nil {
		http.Error(w, "Invalid index format", http.StatusBadRequest)
		return
	}

	log.Printf("Received request to delete SMS index %d...", index)
	err = s.daemon.DeleteSMS(index)
	if err != nil {
		http.Error(w, fmt.Sprintf("Delete failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Trigger dynamic poll to update cache (SMS only)
	s.daemon.PollSMSOnly()

	// Return updated status UI
	s.handleStatus(w, r)
}

type gzipResponseWriter struct {
	http.ResponseWriter
	writer *gzip.Writer
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if w.writer == nil {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length")
		w.writer = gzip.NewWriter(w.ResponseWriter)
	}
	return w.writer.Write(b)
}

func (w *gzipResponseWriter) WriteHeader(statusCode int) {
	if statusCode == http.StatusNotModified || statusCode == http.StatusNoContent {
		w.ResponseWriter.WriteHeader(statusCode)
		return
	}
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Del("Content-Length")
	w.writer = gzip.NewWriter(w.ResponseWriter)
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *gzipResponseWriter) Close() {
	if w.writer != nil {
		w.writer.Close()
	}
}

func gzipHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			h.ServeHTTP(w, r)
			return
		}
		gzw := &gzipResponseWriter{ResponseWriter: w}
		defer gzw.Close()
		h.ServeHTTP(gzw, r)
	})
}
