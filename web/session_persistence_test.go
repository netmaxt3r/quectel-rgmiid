package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionPersistence(t *testing.T) {
	tempDir := t.TempDir()

	s := NewServer(nil, "", "admin", "password", "")
	s.SetDataDir(tempDir)

	// 1. Initially, no sessions
	s.loadSessions()
	if len(s.sessions) != 0 {
		t.Fatalf("expected 0 sessions loaded, got %d", len(s.sessions))
	}

	// 2. Create a session and verify it is saved to disk
	req := httptest.NewRequest("POST", "/login", nil)
	rec := httptest.NewRecorder()
	sessID, err := s.createSession(rec, req)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	sessionFilePath := filepath.Join(tempDir, "sessions.json")
	if _, err := os.Stat(sessionFilePath); err != nil {
		t.Fatalf("expected session file to exist at %s: %v", sessionFilePath, err)
	}

	// Verify the file content
	data, err := os.ReadFile(sessionFilePath)
	if err != nil {
		t.Fatalf("failed to read session file: %v", err)
	}

	var savedSessions map[string]time.Time
	if err := json.Unmarshal(data, &savedSessions); err != nil {
		t.Fatalf("failed to unmarshal saved sessions: %v", err)
	}

	expiry, exists := savedSessions[sessID]
	if !exists {
		t.Fatalf("expected session ID %q to exist in saved file", sessID)
	}

	// 3. Start a new server instance sharing the same data dir, load, and verify it recovers the session
	s2 := NewServer(nil, "", "admin", "password", "")
	s2.SetDataDir(tempDir)
	s2.loadSessions()

	if len(s2.sessions) != 1 {
		t.Fatalf("expected 1 session loaded in new server, got %d", len(s2.sessions))
	}

	loadedExpiry, exists := s2.sessions[sessID]
	if !exists {
		t.Fatalf("expected session ID %q to be loaded", sessID)
	}

	// Compare times (allowing slight delta due to serialization precision if any, but time.Time JSON marshalling maintains nanosecond precision or RFC3339Nano)
	if !loadedExpiry.Equal(expiry) {
		t.Errorf("expected expiry %v, got %v", expiry, loadedExpiry)
	}

	// 4. Test loading ignores expired sessions
	s3 := NewServer(nil, "", "admin", "password", "")
	s3.SetDataDir(tempDir)

	// Manually write expired session to the json file
	expiredSess := map[string]time.Time{
		"expired_id": time.Now().Add(-1 * time.Hour),
		"valid_id":   time.Now().Add(1 * time.Hour),
	}
	expiredData, err := json.Marshal(expiredSess)
	if err != nil {
		t.Fatalf("failed to marshal expired sessions: %v", err)
	}
	if err := os.WriteFile(sessionFilePath, expiredData, 0600); err != nil {
		t.Fatalf("failed to write expired sessions file: %v", err)
	}

	s3.loadSessions()
	if len(s3.sessions) != 1 {
		t.Fatalf("expected only 1 valid session loaded, got %d", len(s3.sessions))
	}
	if _, exists := s3.sessions["valid_id"]; !exists {
		t.Errorf("expected session 'valid_id' to exist")
	}
	if _, exists := s3.sessions["expired_id"]; exists {
		t.Errorf("expected expired session 'expired_id' to be filtered out")
	}

	// 5. Test session deletion updates disk
	s4 := NewServer(nil, "", "admin", "password", "")
	s4.SetDataDir(tempDir)
	s4.loadSessions()

	// Perform a logout / destroy session request
	reqLogout := httptest.NewRequest("GET", "/logout", nil)
	reqLogout.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "valid_id"})
	recLogout := httptest.NewRecorder()

	s4.destroySession(recLogout, reqLogout)

	// Read sessions file again to verify "valid_id" is deleted
	dataAfterDelete, err := os.ReadFile(sessionFilePath)
	if err != nil {
		t.Fatalf("failed to read session file after delete: %v", err)
	}
	var sessionsAfterDelete map[string]time.Time
	if err := json.Unmarshal(dataAfterDelete, &sessionsAfterDelete); err != nil {
		t.Fatalf("failed to unmarshal sessions: %v", err)
	}

	if len(sessionsAfterDelete) != 0 {
		t.Errorf("expected 0 sessions after delete, got %d: %v", len(sessionsAfterDelete), sessionsAfterDelete)
	}
}
