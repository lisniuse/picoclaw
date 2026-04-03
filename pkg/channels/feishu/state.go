package feishu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/fileutil"
)

type directChatsState struct {
	Chats      map[string]string `json:"chats"`
	LastUserID string            `json:"last_user_id,omitempty"`
}

func buildFeishuDirectChatsPath(cfg config.FeishuConfig) string {
	accountKey := strings.TrimSpace(cfg.AppID)
	if accountKey == "" {
		accountKey = "default"
	}
	return filepath.Join(config.GetHome(), "channels", "feishu", "direct-chats", accountKey+".json")
}

func loadDirectChatsState(path string) (directChatsState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return directChatsState{}, nil
		}
		return directChatsState{}, err
	}

	var decoded directChatsState
	if err := json.Unmarshal(data, &decoded); err != nil {
		return directChatsState{}, err
	}
	return decoded, nil
}

func saveDirectChatsState(path string, chats map[string]string, lastUserID string) error {
	data, err := json.Marshal(directChatsState{
		Chats:      chats,
		LastUserID: lastUserID,
	})
	if err != nil {
		return err
	}
	return fileutil.WriteFileAtomic(path, data, 0o600)
}
