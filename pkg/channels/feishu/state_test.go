package feishu

import (
	"path/filepath"
	"testing"
)

func TestSaveAndLoadDirectChatsState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "direct-chats.json")
	chats := map[string]string{
		"ou_a": "oc_direct_a",
		"ou_b": "oc_direct_b",
	}

	if err := saveDirectChatsState(path, chats, "ou_b"); err != nil {
		t.Fatalf("saveDirectChatsState() error = %v", err)
	}

	got, err := loadDirectChatsState(path)
	if err != nil {
		t.Fatalf("loadDirectChatsState() error = %v", err)
	}
	if got.LastUserID != "ou_b" {
		t.Fatalf("LastUserID = %q, want ou_b", got.LastUserID)
	}
	if got.Chats["ou_b"] != "oc_direct_b" {
		t.Fatalf("Chats[ou_b] = %q, want oc_direct_b", got.Chats["ou_b"])
	}
}
