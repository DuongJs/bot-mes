package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"mybot/internal/config"
)

// stubCommands implements CommandLister for testing.
type stubCommands struct {
	commands map[string]string
}

func (s *stubCommands) List() map[string]string {
	return s.commands
}

func newTestServer() *Server {
	cfg := config.New()
	cfg.Modules = map[string]bool{"ping": true, "help": true, "media": false}
	s := New(cfg, nil)
	s.Commands = &stubCommands{commands: map[string]string{
		"ping": "Kiểm tra bot",
		"help": "Xem danh sách lệnh",
	}}
	return s
}

func TestHandleStatus(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	s.handleStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if _, ok := result["uptime"]; !ok {
		t.Error("response missing uptime field")
	}
}

func TestHandleStatusMethodNotAllowed(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/status", nil)
	w := httptest.NewRecorder()
	s.handleStatus(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestHandleCommands(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/commands", nil)
	w := httptest.NewRecorder()
	s.handleCommands(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["ping"] != "Kiểm tra bot" {
		t.Errorf("unexpected ping description: %s", result["ping"])
	}
	if result["help"] != "Xem danh sách lệnh" {
		t.Errorf("unexpected help description: %s", result["help"])
	}
}

func TestHandleCommandsNilCommands(t *testing.T) {
	cfg := config.New()
	s := New(cfg, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/commands", nil)
	w := httptest.NewRecorder()
	s.handleCommands(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestHandleModulesGet(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/modules", nil)
	w := httptest.NewRecorder()
	s.handleModules(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result["ping"] {
		t.Error("expected ping to be true")
	}
	if result["media"] {
		t.Error("expected media to be false")
	}
}

func TestHandleModulesPost(t *testing.T) {
	// Change to temp dir so config.json is written there instead of source tree
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	s := newTestServer()
	body := `{"ping":false,"help":true,"media":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/modules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleModules(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// Verify config was updated
	if s.Config.Modules["ping"] {
		t.Error("expected ping to be false after update")
	}
	if !s.Config.Modules["media"] {
		t.Error("expected media to be true after update")
	}
}

func TestHandleLogs(t *testing.T) {
	s := newTestServer()
	s.Logs.Add("info", "Bot started")
	s.Logs.Add("error", "Connection failed")

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	w := httptest.NewRecorder()
	s.handleLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result []LogEntry
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(result))
	}
	if result[0].Level != "info" || result[0].Message != "Bot started" {
		t.Errorf("unexpected first entry: %+v", result[0])
	}
	if result[1].Level != "error" || result[1].Message != "Connection failed" {
		t.Errorf("unexpected second entry: %+v", result[1])
	}
}

func TestHandleLogsNilBuffer(t *testing.T) {
	cfg := config.New()
	s := &Server{Config: cfg}
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	w := httptest.NewRecorder()
	s.handleLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var result []LogEntry
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty array, got %v", result)
	}
}

func TestLogBufferCapacity(t *testing.T) {
	lb := NewLogBuffer(3)
	lb.Add("info", "msg1")
	lb.Add("info", "msg2")
	lb.Add("info", "msg3")
	lb.Add("info", "msg4") // should evict msg1

	entries := lb.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Message != "msg2" {
		t.Errorf("expected oldest to be msg2, got %s", entries[0].Message)
	}
	if entries[2].Message != "msg4" {
		t.Errorf("expected newest to be msg4, got %s", entries[2].Message)
	}
}

func TestHandleIndex(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	s.handleIndex(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("unexpected content type: %s", ct)
	}
	if !strings.Contains(w.Body.String(), "<!DOCTYPE html>") {
		t.Error("response does not contain HTML doctype")
	}
}

func TestHandleRestart(t *testing.T) {
	cfg := config.New()
	s := &Server{
		Config:  cfg,
		Restart: func() {},
		Logs:    NewLogBuffer(10),
	}
	req := httptest.NewRequest(http.MethodPost, "/api/restart", nil)
	w := httptest.NewRecorder()
	s.handleRestart(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
