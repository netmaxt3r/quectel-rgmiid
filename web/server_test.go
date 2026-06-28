package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rgmii/commands"
)

type mockDaemon struct {
	PollAllFunc        func()
	GetStatusFunc      func() commands.ModemStatus
	SetAPNFunc         func(ctxID int, cfg commands.APNConfig) error
	ActivateDataFunc   func(ctxID int) error
	DeactivateDataFunc func(ctxID int) error
	SendCommandFunc    func(cmd string) (string, error)
	SendSMSFunc        func(number, text string) error
	DeleteSMSFunc      func(index int) error
	PollSMSOnlyFunc    func()

	Status            commands.ModemStatus
	SetAPNErr         error
	ActivateDataErr   error
	DeactivateDataErr error
	SendCommandResp   string
	SendCommandErr    error
	SendSMSErr        error
	DeleteSMSErr      error

	PollAllCalls   int
	GetStatusCalls int
	SetAPNCalls    []struct {
		CtxID int
		Cfg   commands.APNConfig
	}
	ActivateDataCalls   []int
	DeactivateDataCalls []int
	SendCommandCalls    []string
	SendSMSCalls        []struct {
		Number string
		Text   string
	}
	DeleteSMSCalls   []int
	PollSMSOnlyCalls int
}

func (m *mockDaemon) PollAll() {
	m.PollAllCalls++
	if m.PollAllFunc != nil {
		m.PollAllFunc()
	}
}

func (m *mockDaemon) GetStatus() commands.ModemStatus {
	m.GetStatusCalls++
	if m.GetStatusFunc != nil {
		return m.GetStatusFunc()
	}
	if m.Status.RawResponses == nil {
		m.Status.RawResponses = make(map[string]string)
	}
	if m.Status.APNConfigMap == nil {
		m.Status.APNConfigMap = make(map[int]commands.APNConfig)
	}
	return m.Status
}

func (m *mockDaemon) SetAPN(ctxID int, cfg commands.APNConfig) error {
	m.SetAPNCalls = append(m.SetAPNCalls, struct {
		CtxID int
		Cfg   commands.APNConfig
	}{ctxID, cfg})
	if m.SetAPNFunc != nil {
		return m.SetAPNFunc(ctxID, cfg)
	}
	return m.SetAPNErr
}

func (m *mockDaemon) ActivateData(ctxID int) error {
	m.ActivateDataCalls = append(m.ActivateDataCalls, ctxID)
	if m.ActivateDataFunc != nil {
		return m.ActivateDataFunc(ctxID)
	}
	return m.ActivateDataErr
}

func (m *mockDaemon) DeactivateData(ctxID int) error {
	m.DeactivateDataCalls = append(m.DeactivateDataCalls, ctxID)
	if m.DeactivateDataFunc != nil {
		return m.DeactivateDataFunc(ctxID)
	}
	return m.DeactivateDataErr
}

func (m *mockDaemon) SendCommand(cmd string) (string, error) {
	m.SendCommandCalls = append(m.SendCommandCalls, cmd)
	if m.SendCommandFunc != nil {
		return m.SendCommandFunc(cmd)
	}
	return m.SendCommandResp, m.SendCommandErr
}

func (m *mockDaemon) SendSMS(number, text string) error {
	m.SendSMSCalls = append(m.SendSMSCalls, struct {
		Number string
		Text   string
	}{number, text})
	if m.SendSMSFunc != nil {
		return m.SendSMSFunc(number, text)
	}
	return m.SendSMSErr
}

func (m *mockDaemon) DeleteSMS(index int) error {
	m.DeleteSMSCalls = append(m.DeleteSMSCalls, index)
	if m.DeleteSMSFunc != nil {
		return m.DeleteSMSFunc(index)
	}
	return m.DeleteSMSErr
}

func (m *mockDaemon) PollSMSOnly() {
	m.PollSMSOnlyCalls++
	if m.PollSMSOnlyFunc != nil {
		m.PollSMSOnlyFunc()
	}
}

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
		req.Header.Set("HX-Request", "true")
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
		req.Header.Set("HX-Request", "true")
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
		req.Header.Set("HX-Request", "true")
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
		d := &mockDaemon{}
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

func TestLoginLogout(t *testing.T) {
	t.Run("HTMX login and logout flow", func(t *testing.T) {
		s := NewServer(nil, "", "admin", "password", "")
		mux := http.NewServeMux()
		s.routes(mux)

		// 1. GET /login should render the login page
		req := httptest.NewRequest("GET", "/login", nil)
		req.Header.Set("Accept", "text/html")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET /login expected 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "RGMII Control Panel") {
			t.Errorf("expected body to contain title, got %q", rec.Body.String())
		}

		// 2. POST /login with invalid credentials should return 401
		req = httptest.NewRequest("POST", "/login", strings.NewReader("username=admin&password=wrong"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "text/html")
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("POST /login invalid expected 401, got %d", rec.Code)
		}

		// 3. POST /login with valid credentials should redirect to /
		req = httptest.NewRequest("POST", "/login", strings.NewReader("username=admin&password=password"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "text/html")
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Errorf("POST /login valid expected 303, got %d", rec.Code)
		}
		cookie := rec.Result().Cookies()
		if len(cookie) == 0 || cookie[0].Name != sessionCookieName {
			t.Errorf("expected session cookie, got none")
		}

		// 4. GET /logout should clear the session cookie
		req = httptest.NewRequest("GET", "/logout", nil)
		req.Header.Set("Accept", "text/html")
		req.AddCookie(cookie[0])
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Errorf("GET /logout expected 303, got %d", rec.Code)
		}
		logoutCookies := rec.Result().Cookies()
		if len(logoutCookies) == 0 || logoutCookies[0].Value != "" {
			t.Errorf("expected empty session cookie, got %+v", logoutCookies)
		}
	})

	t.Run("JSON login and logout flow", func(t *testing.T) {
		s := NewServer(nil, "", "admin", "password", "")
		mux := http.NewServeMux()
		s.routes(mux)

		// 1. GET /login should return JSON guide
		req := httptest.NewRequest("GET", "/login", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET /login JSON expected 200, got %d", rec.Code)
		}
		var getResp map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&getResp); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(getResp["message"], "Please POST") {
			t.Errorf("unexpected JSON response: %+v", getResp)
		}

		// 2. POST /login with invalid credentials should return 401 JSON
		body, _ := json.Marshal(map[string]string{"username": "admin", "password": "wrong"})
		req = httptest.NewRequest("POST", "/login", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("POST /login JSON invalid expected 401, got %d", rec.Code)
		}
		var errResp map[string]interface{}
		json.NewDecoder(rec.Body).Decode(&errResp)
		if errResp["error"] != "Invalid username or password" {
			t.Errorf("unexpected error payload: %+v", errResp)
		}

		// 3. POST /login with valid credentials should return 200 and set cookie
		body, _ = json.Marshal(map[string]string{"username": "admin", "password": "password"})
		req = httptest.NewRequest("POST", "/login", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("POST /login JSON valid expected 200, got %d", rec.Code)
		}
		var okResp map[string]interface{}
		json.NewDecoder(rec.Body).Decode(&okResp)
		if okResp["status"] != "success" {
			t.Errorf("unexpected success payload: %+v", okResp)
		}
		cookie := rec.Result().Cookies()
		if len(cookie) == 0 || cookie[0].Name != sessionCookieName {
			t.Errorf("expected session cookie in JSON response, got none")
		}

		// 4. GET /logout should return 200 JSON and clear cookie
		req = httptest.NewRequest("GET", "/logout", nil)
		req.AddCookie(cookie[0])
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET /logout JSON expected 200, got %d", rec.Code)
		}
		var logoutResp map[string]interface{}
		json.NewDecoder(rec.Body).Decode(&logoutResp)
		if logoutResp["status"] != "success" {
			t.Errorf("unexpected logout payload: %+v", logoutResp)
		}
	})
}
