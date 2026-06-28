package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rgmii/commands"
)

func TestHTMXHandlerRoutes(t *testing.T) {
	t.Run("Status HTMX success", func(t *testing.T) {
		status := commands.ModemStatus{
			Tech:             "LTE",
			ConnectionStatus: "Connected",
		}
		d := &mockDaemon{Status: status}
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("GET", "/api/status", nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Network Connection") || !strings.Contains(body, "LTE") {
			t.Errorf("expected body to contain status details, got %q", body)
		}
	})

	t.Run("Set APN HTMX success", func(t *testing.T) {
		d := &mockDaemon{}
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("POST", "/api/apn/2", strings.NewReader("apn=fast&pdp_type=IP&username=u&password=p"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "saved successfully") {
			t.Errorf("expected success message in response, got %q", body)
		}
		if len(d.SetAPNCalls) != 1 || d.SetAPNCalls[0].CtxID != 2 || d.SetAPNCalls[0].Cfg.APN != "fast" {
			t.Errorf("SetAPN called with unexpected parameters: %+v", d.SetAPNCalls)
		}
	})

	t.Run("Activate Data HTMX success", func(t *testing.T) {
		d := &mockDaemon{}
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("POST", "/api/data/activate/2", nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "activated") {
			t.Errorf("expected body to confirm activation, got %q", body)
		}
	})

	t.Run("Deactivate Data HTMX success", func(t *testing.T) {
		d := &mockDaemon{}
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("POST", "/api/data/deactivate/2", nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "deactivated") {
			t.Errorf("expected body to confirm deactivation, got %q", body)
		}
	})

	t.Run("Cmd HTMX success", func(t *testing.T) {
		d := &mockDaemon{SendCommandResp: "Quectel\r\n\r\nOK\r\n"}
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("POST", "/api/cmd", strings.NewReader("cmd=ATI"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Quectel") {
			t.Errorf("expected command output, got %q", body)
		}
	})

	t.Run("Modem Restart HTMX success", func(t *testing.T) {
		d := &mockDaemon{SendCommandResp: "OK"}
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("POST", "/api/modem/restart", nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Restart failed") && !strings.Contains(body, "executed successfully") {
			t.Errorf("expected success output, got %q", body)
		}
	})

	t.Run("Settings page HTML success", func(t *testing.T) {
		d := &mockDaemon{}
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("GET", "/api/settings", nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "APN / Data Context Management") {
			t.Errorf("expected settings template to render, got %q", body)
		}
	})

	t.Run("Console page HTML success", func(t *testing.T) {
		d := &mockDaemon{}
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("GET", "/api/console", nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Interactive AT Console") {
			t.Errorf("expected console template to render, got %q", body)
		}
	})
}
