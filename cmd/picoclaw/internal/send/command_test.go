package send

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestResolveMessageContent_FromFlag(t *testing.T) {
	got, err := resolveMessageContent("hello", "")
	if err != nil {
		t.Fatalf("resolveMessageContent() error = %v", err)
	}
	if got != "hello" {
		t.Fatalf("resolveMessageContent() = %q, want hello", got)
	}
}

func TestResolveMessageContent_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "message.txt")
	want := "first line\nsecond line\n"
	if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := resolveMessageContent("", path)
	if err != nil {
		t.Fatalf("resolveMessageContent() error = %v", err)
	}
	if got != want {
		t.Fatalf("resolveMessageContent() = %q, want %q", got, want)
	}
}

func TestResolveMessageContent_RejectsConflictingFlags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "message.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := resolveMessageContent("hello", path)
	if err == nil {
		t.Fatal("resolveMessageContent() expected conflict error")
	}
	if !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("resolveMessageContent() error = %v, want conflict hint", err)
	}
}

func TestResolveMessageContent_RejectsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "message.txt")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := resolveMessageContent("", path)
	if err == nil {
		t.Fatal("resolveMessageContent() expected empty file error")
	}
	if !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("resolveMessageContent() error = %v, want empty file hint", err)
	}
}

func TestResolveLastUserID_UsesPersistedLastUserID(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "channels", "weixin", "context-tokens")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	path := filepath.Join(dir, "acct.json")
	data := `{"tokens":{"user-a":"token-a","user-b":"token-b"},"last_user_id":"user-b"}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := resolveLastUserID(home, "weixin")
	if err != nil {
		t.Fatalf("resolveLastUserID() error = %v", err)
	}
	if got != "user-b" {
		t.Fatalf("resolveLastUserID() = %q, want user-b", got)
	}
}

func TestResolveLastUserID_SingleTokenFallback(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "channels", "weixin", "context-tokens")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	path := filepath.Join(dir, "acct.json")
	data := `{"tokens":{"solo-user":"token-a"}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := resolveLastUserID(home, "weixin")
	if err != nil {
		t.Fatalf("resolveLastUserID() error = %v", err)
	}
	if got != "solo-user" {
		t.Fatalf("resolveLastUserID() = %q, want solo-user", got)
	}
}

func TestResolveLastUserID_RejectsAmbiguousState(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "channels", "weixin", "context-tokens")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	path := filepath.Join(dir, "acct.json")
	data := `{"tokens":{"user-a":"token-a","user-b":"token-b"}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := resolveLastUserID(home, "weixin")
	if err == nil {
		t.Fatal("resolveLastUserID() expected error for ambiguous state")
	}
	if !strings.Contains(err.Error(), "multiple user IDs") {
		t.Fatalf("resolveLastUserID() error = %v, want ambiguity hint", err)
	}
}

func TestResolveGatewaySendURL_FallsBackToConfigWhenPIDMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("path = %q, want /health", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port, err := parseHostPort(server.URL)
	if err != nil {
		t.Fatalf("parseHostPort() error = %v", err)
	}

	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	data := `{"version":2,"gateway":{"host":"` + host + `","port":` + strconv.Itoa(port) + `}}`
	if err := os.WriteFile(configPath, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := resolveGatewaySendURL(home, configPath, server.Client())
	if err != nil {
		t.Fatalf("resolveGatewaySendURL() error = %v", err)
	}
	want := server.URL + "/v1/send"
	if got != want {
		t.Fatalf("resolveGatewaySendURL() = %q, want %q", got, want)
	}
}

func TestResolveGatewaySendURL_ConfigFallbackRequiresReachableGateway(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	data := `{"version":2,"gateway":{"host":"127.0.0.1","port":1}}`
	if err := os.WriteFile(configPath, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := resolveGatewaySendURL(home, configPath, &http.Client{Timeout: 200 * time.Millisecond})
	if err == nil {
		t.Fatal("resolveGatewaySendURL() expected error for unreachable configured gateway")
	}
	if !strings.Contains(err.Error(), "configured gateway") {
		t.Fatalf("resolveGatewaySendURL() error = %v, want configured gateway hint", err)
	}
}
