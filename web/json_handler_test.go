package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"rgmii/commands"
)

func TestJSONHandlerRoutes(t *testing.T) {
	t.Run("Status endpoint success", func(t *testing.T) {
		status := commands.ModemStatus{
			Tech:             "5G-SA",
			ConnectionStatus: "Connected",
		}
		d := &mockDaemon{Status: status}
		s := NewServer(d, "192.168.225.1:1555", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("GET", "/api/status", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		var resp commands.ModemStatus
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if resp.Tech != "5G-SA" || len(resp.RawResponses) != 0 {
			t.Errorf("unexpected status response (expected raw_responses to be empty): %+v", resp)
		}
	})

	t.Run("Get APN success", func(t *testing.T) {
		status := commands.ModemStatus{
			APNConfigMap: map[int]commands.APNConfig{
				1: {APN: "internet", PDPType: "IP"},
			},
		}
		d := &mockDaemon{Status: status}
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("GET", "/api/apn/1", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		var resp commands.APNConfig
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if resp.APN != "internet" || resp.PDPType != "IP" {
			t.Errorf("unexpected APN config: %+v", resp)
		}
	})

	t.Run("Get APN not found", func(t *testing.T) {
		d := &mockDaemon{}
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("GET", "/api/apn/99", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("Set APN success", func(t *testing.T) {
		d := &mockDaemon{}
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		cfg := commands.APNConfig{APN: "fast.t-mobile.com", PDPType: "IPV4V6"}
		body, _ := json.Marshal(cfg)
		req := httptest.NewRequest("POST", "/api/apn/1", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		if len(d.SetAPNCalls) != 1 || d.SetAPNCalls[0].CtxID != 1 || d.SetAPNCalls[0].Cfg.APN != "fast.t-mobile.com" {
			t.Errorf("SetAPN was not called with correct parameters: %+v", d.SetAPNCalls)
		}
	})

	t.Run("Activate Data success", func(t *testing.T) {
		d := &mockDaemon{}
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("POST", "/api/data/activate/2", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		if len(d.ActivateDataCalls) != 1 || d.ActivateDataCalls[0] != 2 {
			t.Errorf("ActivateData was not called with correct context ID: %+v", d.ActivateDataCalls)
		}
	})

	t.Run("Deactivate Data success", func(t *testing.T) {
		d := &mockDaemon{}
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("POST", "/api/data/deactivate/3", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		if len(d.DeactivateDataCalls) != 1 || d.DeactivateDataCalls[0] != 3 {
			t.Errorf("DeactivateData was not called with correct context ID: %+v", d.DeactivateDataCalls)
		}
	})

	t.Run("Cmd success", func(t *testing.T) {
		d := &mockDaemon{SendCommandResp: "AT+CSQ\r\n+CSQ: 31,99\r\n\r\nOK\r\n"}
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		body, _ := json.Marshal(cmdRequest{Cmd: "AT+CSQ"})
		req := httptest.NewRequest("POST", "/api/cmd", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		var resp cmdResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if !resp.Success || resp.Response != "AT+CSQ\r\n+CSQ: 31,99\r\n\r\nOK\r\n" {
			t.Errorf("unexpected command response: %+v", resp)
		}
	})

	t.Run("SMS Get success", func(t *testing.T) {
		status := commands.ModemStatus{
			SMSList: commands.SMSList{
				SMS: []commands.SMSMessage{
					{Index: 1, Content: "Hello test"},
				},
			},
		}
		d := &mockDaemon{Status: status}
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("GET", "/api/sms", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		var resp commands.SMSList
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.SMS) != 1 || resp.SMS[0].Content != "Hello test" {
			t.Errorf("unexpected SMS response: %+v", resp)
		}
	})

	t.Run("SMS Send JSON success", func(t *testing.T) {
		d := &mockDaemon{}
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		body := map[string]string{"number": "+1234567890", "text": "hello test"}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/sms/send", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		if len(d.SendSMSCalls) != 1 || d.SendSMSCalls[0].Number != "+1234567890" || d.SendSMSCalls[0].Text != "hello test" {
			t.Errorf("SendSMS was not called with correct details: %+v", d.SendSMSCalls)
		}
		if d.PollSMSOnlyCalls != 1 {
			t.Errorf("expected PollSMSOnly to be called once, got %d", d.PollSMSOnlyCalls)
		}
	})

	t.Run("SMS Delete success", func(t *testing.T) {
		d := &mockDaemon{}
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		body := map[string]int{"index": 5}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/sms/delete", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		if len(d.DeleteSMSCalls) != 1 || d.DeleteSMSCalls[0] != 5 {
			t.Errorf("DeleteSMS was not called with index 5, calls: %+v", d.DeleteSMSCalls)
		}
	})

	t.Run("Modem Restart success", func(t *testing.T) {
		d := &mockDaemon{SendCommandResp: "OK"}
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("POST", "/api/modem/restart", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		if len(d.SendCommandCalls) != 1 || d.SendCommandCalls[0] != "AT+CFUN=1,1" {
			t.Errorf("expected SendCommand with 'AT+CFUN=1,1', got: %+v", d.SendCommandCalls)
		}
	})

	t.Run("Cmd wrong method", func(t *testing.T) {
		s := NewServer(nil, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("GET", "/api/cmd", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", rec.Code)
		}
	})

	t.Run("Cmd missing content-type", func(t *testing.T) {
		s := NewServer(nil, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("POST", "/api/cmd", bytes.NewBufferString("{}"))
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("Cmd invalid json request", func(t *testing.T) {
		s := NewServer(nil, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		req := httptest.NewRequest("POST", "/api/cmd", bytes.NewBufferString("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("Cmd empty command", func(t *testing.T) {
		s := NewServer(nil, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		body, _ := json.Marshal(map[string]string{"cmd": ""})
		req := httptest.NewRequest("POST", "/api/cmd", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("Cmd daemon offline error", func(t *testing.T) {
		d := &mockDaemon{SendCommandErr: fmt.Errorf("modem connection offline")}
		s := NewServer(d, "", "", "", "")
		mux := http.NewServeMux()
		s.routes(mux)

		body, _ := json.Marshal(map[string]string{"cmd": "AT+CSQ"})
		req := httptest.NewRequest("POST", "/api/cmd", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}
	})
}
