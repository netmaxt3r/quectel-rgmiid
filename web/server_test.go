package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rgmii/daemon"
)

func TestSessionAndTokenAuth(t *testing.T) {
	// Create a mock handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	t.Run("no auth configured", func(t *testing.T) {
		s := NewServer(nil, "", "", "", "")
		authHandler := s.sessionOrTokenAuth(handler)

		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()

		authHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		if rec.Body.String() != "OK" {
			t.Errorf("expected Body 'OK', got %q", rec.Body.String())
		}
	})

	t.Run("auth configured - unauthenticated index redirects to /login", func(t *testing.T) {
		s := NewServer(nil, "", "user", "pass", "")
		authHandler := s.sessionOrTokenAuth(handler)

		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()

		authHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("expected 303 Redirect, got %d", rec.Code)
		}
		if rec.Header().Get("Location") != "/login" {
			t.Errorf("expected redirect to /login, got %q", rec.Header().Get("Location"))
		}
	})

	t.Run("auth configured - unauthenticated API returns 401", func(t *testing.T) {
		s := NewServer(nil, "", "user", "pass", "")
		authHandler := s.sessionOrTokenAuth(handler)

		req := httptest.NewRequest("GET", "/api/status", nil)
		rec := httptest.NewRecorder()

		authHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("auth configured - unauthenticated request with text/html Accept header redirects to /login", func(t *testing.T) {
		s := NewServer(nil, "", "user", "pass", "")
		authHandler := s.sessionOrTokenAuth(handler)

		req := httptest.NewRequest("GET", "/anypath", nil)
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
		rec := httptest.NewRecorder()

		authHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("expected 303 Redirect, got %d", rec.Code)
		}
		if rec.Header().Get("Location") != "/login" {
			t.Errorf("expected redirect to /login, got %q", rec.Header().Get("Location"))
		}
	})

	t.Run("auth configured - unauthenticated HTMX request returns 401 with HX-Redirect header", func(t *testing.T) {
		s := NewServer(nil, "", "user", "pass", "")
		authHandler := s.sessionOrTokenAuth(handler)

		req := httptest.NewRequest("GET", "/api/status", nil)
		req.Header.Set("Accept", "text/html")
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()

		authHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
		if rec.Header().Get("HX-Redirect") != "/login" {
			t.Errorf("expected HX-Redirect header to /login, got %q", rec.Header().Get("HX-Redirect"))
		}
	})

	t.Run("auth configured - valid session allowed", func(t *testing.T) {
		s := NewServer(nil, "", "user", "pass", "")
		authHandler := s.sessionOrTokenAuth(handler)

		// Create session
		sessID := "testsession"
		s.sessions[sessID] = time.Now().Add(1 * time.Hour)

		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessID})
		rec := httptest.NewRecorder()

		authHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("auth configured - expired session rejected", func(t *testing.T) {
		s := NewServer(nil, "", "user", "pass", "")
		authHandler := s.sessionOrTokenAuth(handler)

		// Create expired session
		sessID := "expiredsession"
		s.sessions[sessID] = time.Now().Add(-1 * time.Hour)

		req := httptest.NewRequest("GET", "/api/status", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessID})
		rec := httptest.NewRecorder()

		authHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("auth configured - valid Authorization token allowed", func(t *testing.T) {
		s := NewServer(nil, "", "user", "pass", "mysecretkey")
		authHandler := s.sessionOrTokenAuth(handler)

		req := httptest.NewRequest("GET", "/api/status", nil)
		req.Header.Set("Authorization", "Bearer mysecretkey")
		rec := httptest.NewRecorder()

		authHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("auth configured - valid X-API-Key token allowed", func(t *testing.T) {
		s := NewServer(nil, "", "user", "pass", "mysecretkey")
		authHandler := s.sessionOrTokenAuth(handler)

		req := httptest.NewRequest("GET", "/api/status", nil)
		req.Header.Set("X-API-Key", "mysecretkey")
		rec := httptest.NewRecorder()

		authHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("auth configured - invalid token rejected", func(t *testing.T) {
		s := NewServer(nil, "", "user", "pass", "mysecretkey")
		authHandler := s.sessionOrTokenAuth(handler)

		req := httptest.NewRequest("GET", "/api/status", nil)
		req.Header.Set("Authorization", "Bearer wrongkey")
		rec := httptest.NewRecorder()

		authHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
	})
}

func TestCmdJSON(t *testing.T) {
	t.Run("wrong method", func(t *testing.T) {
		s := NewServer(nil, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("GET", "/api/cmd/json", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", rec.Code)
		}
	})

	t.Run("missing content-type", func(t *testing.T) {
		s := NewServer(nil, "", "", "", "")
		req := httptest.NewRequest("POST", "/api/cmd/json", bytes.NewBufferString("{}"))
		rec := httptest.NewRecorder()

		s.handleCmdJSON(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
		var resp cmdResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if resp.Success != false || resp.Error != "Content-Type must be application/json" {
			t.Errorf("unexpected response: %+v", resp)
		}
	})

	t.Run("invalid json request", func(t *testing.T) {
		s := NewServer(nil, "", "", "", "")
		req := httptest.NewRequest("POST", "/api/cmd/json", bytes.NewBufferString("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		s.handleCmdJSON(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
		var resp cmdResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if resp.Success != false || resp.Error != "Invalid JSON request" {
			t.Errorf("unexpected response: %+v", resp)
		}
	})

	t.Run("empty command", func(t *testing.T) {
		s := NewServer(nil, "", "", "", "")
		body, _ := json.Marshal(cmdRequest{Cmd: ""})
		req := httptest.NewRequest("POST", "/api/cmd/json", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		s.handleCmdJSON(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
		var resp cmdResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if resp.Success != false || resp.Error != "Command is empty" {
			t.Errorf("unexpected response: %+v", resp)
		}
	})

	t.Run("daemon offline error", func(t *testing.T) {
		d := daemon.NewDaemon("127.0.0.1:0", 0)
		s := NewServer(d, "", "", "", "")
		body, _ := json.Marshal(cmdRequest{Cmd: "AT+CSQ"})
		req := httptest.NewRequest("POST", "/api/cmd/json", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		s.handleCmdJSON(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}
		var resp cmdResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if resp.Success != false || resp.Error != "modem connection offline" {
			t.Errorf("unexpected response: %+v", resp)
		}
	})
}

func TestCSRFProtect(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	t.Run("GET request - allowed", func(t *testing.T) {
		s := NewServer(nil, "", "", "", "")
		csrfHandler := s.csrfProtect(handler)

		req := httptest.NewRequest("GET", "/api/cmd", nil)
		req.Header.Set("Origin", "http://attacker.com")
		rec := httptest.NewRecorder()

		csrfHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("POST request - same origin - allowed", func(t *testing.T) {
		s := NewServer(nil, "", "", "", "")
		csrfHandler := s.csrfProtect(handler)

		req := httptest.NewRequest("POST", "/api/cmd", nil)
		req.Host = "localhost:8080"
		req.Header.Set("Origin", "http://localhost:8080")
		rec := httptest.NewRecorder()

		csrfHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("POST request - cross origin - forbidden", func(t *testing.T) {
		s := NewServer(nil, "", "", "", "")
		csrfHandler := s.csrfProtect(handler)

		req := httptest.NewRequest("POST", "/api/cmd", nil)
		req.Host = "localhost:8080"
		req.Header.Set("Origin", "http://attacker.com")
		rec := httptest.NewRecorder()

		csrfHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", rec.Code)
		}
	})

	t.Run("POST request - missing origin and referer - allowed", func(t *testing.T) {
		s := NewServer(nil, "", "", "", "")
		csrfHandler := s.csrfProtect(handler)

		req := httptest.NewRequest("POST", "/api/cmd", nil)
		req.Host = "localhost:8080"
		rec := httptest.NewRecorder()

		csrfHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
	})
}

func TestDebugEndpoint(t *testing.T) {
	t.Run("QUECTEL_DEBUG not set", func(t *testing.T) {
		t.Setenv("QUECTEL_DEBUG", "")
		s := NewServer(nil, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("GET", "/api/debug", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("QUECTEL_DEBUG set to 0", func(t *testing.T) {
		t.Setenv("QUECTEL_DEBUG", "0")
		s := NewServer(nil, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("GET", "/api/debug", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("QUECTEL_DEBUG set to 1", func(t *testing.T) {
		t.Setenv("QUECTEL_DEBUG", "1")
		d := daemon.NewDaemon("", 0)
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("GET", "/api/debug", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		if rec.Header().Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %q", rec.Header().Get("Content-Type"))
		}
	})
}

func TestDeleteSMS(t *testing.T) {
	t.Run("missing index", func(t *testing.T) {
		s := NewServer(nil, "", "", "", "")
		req := httptest.NewRequest("POST", "/api/sms/delete", nil)
		rec := httptest.NewRecorder()

		s.handleDeleteSMS(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("invalid index format", func(t *testing.T) {
		s := NewServer(nil, "", "", "", "")
		req := httptest.NewRequest("POST", "/api/sms/delete", strings.NewReader("index=abc"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		s.handleDeleteSMS(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("daemon offline returns 500", func(t *testing.T) {
		d := daemon.NewDaemon("127.0.0.1:0", 0)
		s := NewServer(d, "", "", "", "")
		req := httptest.NewRequest("POST", "/api/sms/delete", strings.NewReader("index=3"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		s.handleDeleteSMS(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Delete failed:") {
			t.Errorf("expected error message to contain 'Delete failed:', got %q", rec.Body.String())
		}
	})
}
