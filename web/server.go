package web

import (
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"rgmii/commands"
)

//go:embed templates/* static/*
var webFS embed.FS

var (
	tmpl = template.Must(template.New("").Funcs(template.FuncMap{
		"splitCSV": func(s string) []string {
			if s == "" {
				return nil
			}
			return strings.Split(s, ",")
		},
		"add": func(a, b int) int {
			return a + b
		},
		"safeID": func(s string) string {
			var builder strings.Builder
			for _, r := range s {
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
					builder.WriteRune(r)
				} else {
					builder.WriteRune('_')
				}
			}
			return builder.String()
		},
	}).ParseFS(webFS, "templates/index.html", "templates/status.html", "templates/sms.html", "templates/console.html", "templates/login.html", "templates/settings.html", "templates/dynconfig.html"))
)

// ModemDaemon defines the interface for communicating with the modem daemon.
type ModemDaemon interface {
	PollAll()
	GetStatus() commands.ModemStatus
	SetAPN(ctxID int, cfg commands.APNConfig) error
	ActivateData(ctxID int) error
	DeactivateData(ctxID int) error
	SendCommand(cmd string) (string, error)
	SendSMS(number, text string) error
	DeleteSMS(index int) error
	PollSMSOnly()
	StartInteractive() (commands.InteractiveSession, error)
	GetDynamicConfigs() []string
	GetDynamicConfigState(name string) (*commands.DynamicConfigState, bool)
	QueryDynamicConfigValue(name, subname string) ([]string, string, error)
	SetDynamicConfigValue(name, subname, args string) ([]string, string, error)
}

// Server coordinates routing HTTP requests.
type Server struct {
	daemon      ModemDaemon
	modemAddr   string
	authUser    string
	authPass    string
	apiKey      string
	sessions    map[string]time.Time
	sessMutex   sync.RWMutex
	jsonHandler RequestHandler
	htmxHandler RequestHandler
	httpServer  *http.Server
}

// NewServer creates a new HTTP dashboard server.
func NewServer(daemon ModemDaemon, modemAddr, authUser, authPass, apiKey string) *Server {
	s := &Server{
		daemon:    daemon,
		modemAddr: modemAddr,
		authUser:  authUser,
		authPass:  authPass,
		apiKey:    apiKey,
		sessions:  make(map[string]time.Time),
	}
	s.jsonHandler = NewJSONHandler(s)
	s.htmxHandler = NewHTMXHandler(s)
	return s
}

// Start registers handlers and binds to the specified port.
func (s *Server) Start(port string) error {
	mux := http.NewServeMux()
	s.routes(mux)
	slog.Info("Starting web control panel", "url", "http://localhost:"+port)

	s.httpServer = &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start periodic session cleanup
	go s.startSessionCleanup()

	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// startSessionCleanup periodically removes expired sessions from memory.
func (s *Server) startSessionCleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.sessMutex.Lock()
		now := time.Now()
		expired := 0
		for id, expiry := range s.sessions {
			if now.After(expiry) {
				delete(s.sessions, id)
				expired++
			}
		}
		s.sessMutex.Unlock()
		if expired > 0 {
			slog.Debug("Cleaned up expired sessions", "count", expired)
		}
	}
}

func (s *Server) routes(mux *http.ServeMux) {
	mux.Handle("GET /login", s.dispatch(RequestHandler.HandleLoginGet))
	mux.Handle("POST /login", s.dispatch(RequestHandler.HandleLoginPost))
	mux.Handle("GET /logout", s.dispatch(RequestHandler.HandleLogout))

	// Serve static files with gzip compression - EXEMPTED from auth!
	mux.Handle("GET /static/", gzipHandler(http.FileServer(http.FS(webFS))))

	// Index page (protected)
	mux.Handle("GET /{$}", s.sessionOrTokenAuth(http.HandlerFunc(s.handleIndex)))

	mux.Handle("GET /refresh", s.sessionOrTokenAuth(http.HandlerFunc(s.handleRefresh)))

	// API sub-router (protected)
	apiMux := http.NewServeMux()

	//Status
	apiMux.HandleFunc("GET /api/status", s.dispatch(RequestHandler.HandleStatus))
	apiMux.HandleFunc("POST /api/modem/restart", s.dispatch(RequestHandler.HandleModemRestart))

	// SMS
	apiMux.HandleFunc("GET /api/sms", s.dispatch(RequestHandler.HandleSMSGet))
	apiMux.HandleFunc("POST /api/sms/send", s.dispatch(RequestHandler.HandleSMSSend))
	apiMux.HandleFunc("POST /api/sms/delete", s.dispatch(RequestHandler.HandleSMSDelete))

	// Settings
	apiMux.HandleFunc("GET /api/settings", s.dispatch(RequestHandler.HandleSettingsPage))
	apiMux.HandleFunc("GET /api/apn/{cid}", s.dispatch(RequestHandler.HandleGetAPN))
	apiMux.HandleFunc("POST /api/apn/{cid}", s.dispatch(RequestHandler.HandleSetAPN))
	apiMux.HandleFunc("POST /api/data/activate/{cid}", s.dispatch(RequestHandler.HandleActivateData))
	apiMux.HandleFunc("POST /api/data/deactivate/{cid}", s.dispatch(RequestHandler.HandleDeactivateData))

	// Console
	apiMux.HandleFunc("GET /api/console", s.dispatch(RequestHandler.HandleConsole))
	apiMux.HandleFunc("POST /api/cmd", s.dispatch(RequestHandler.HandleCmd))
	if os.Getenv("QUECTEL_DEBUG") == "1" {
		apiMux.HandleFunc("GET /api/debug", s.dispatch(RequestHandler.HandleDebug))
	}

	// Dynamic Config
	apiMux.HandleFunc("GET /api/dynconfig", s.dispatch(RequestHandler.HandleDynConfigs))
	apiMux.HandleFunc("GET /api/dynconfig/{name}", s.dispatch(RequestHandler.HandleDynConfig))
	apiMux.HandleFunc("POST /api/dynconfig/{name}/get", s.dispatch(RequestHandler.HandleDynConfigGet))
	apiMux.HandleFunc("POST /api/dynconfig/{name}/set", s.dispatch(RequestHandler.HandleDynConfigSet))

	// Mount apiMux with authentication and CSRF protection
	mux.Handle("/api/", s.sessionOrTokenAuth(s.csrfProtect(apiMux)))
}

func (s *Server) renderTemplate(w http.ResponseWriter, status int, name string, data interface{}) {
	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, name, data)
	if err != nil {
		slog.Error("Template render error", "template", name, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := buf.WriteTo(w); err != nil {
		slog.Debug("Socket write error", "template", name, "error", err)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"AuthEnabled":    s.authUser != "" && s.authPass != "",
		"DynamicConfigs": commands.GetDynamicConfigs(),
	}
	s.renderTemplate(w, http.StatusOK, "index.html", data)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	s.daemon.PollAll()
	tab := r.URL.Query().Get("tab")
	switch tab {
	case "sms":
		s.htmxHandler.HandleSMSGet(w, r)
	case "console":
		s.htmxHandler.HandleConsole(w, r)
	case "settings":
		s.htmxHandler.HandleSettingsPage(w, r)
	case "dynconfig":
		s.htmxHandler.HandleDynConfig(w, r)
	default:
		s.htmxHandler.HandleStatus(w, r)
	}
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
