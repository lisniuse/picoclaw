package send

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sipeed/picoclaw/cmd/picoclaw/internal"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/pid"
)

type contextTokensState struct {
	Tokens     map[string]string `json:"tokens"`
	LastUserID string            `json:"last_user_id,omitempty"`
}

type directChatsState struct {
	Chats      map[string]string `json:"chats"`
	LastUserID string            `json:"last_user_id,omitempty"`
}

func NewSendCommand() *cobra.Command {
	var (
		channel     string
		to          string
		message     string
		messageFile string
	)

	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send a message directly to a channel",
		Long: "Send a message directly to a configured channel.\n\n" +
			"For some channels, such as Weixin, this behaves like a reply against a recent active conversation context " +
			"rather than an unlimited proactive push. If the remote user has not messaged the bot recently, the send may fail " +
			"until they send a fresh message first.",
		Example: `  picoclaw send -m "Hello!" --channel weixin
  picoclaw send -m "Hello!" --channel weixin --to "user_id"
  picoclaw send --message-file note.txt --channel weixin`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if channel == "" {
				return fmt.Errorf("--channel is required")
			}

			resolvedMessage, err := resolveMessageContent(message, messageFile)
			if err != nil {
				return err
			}

			homePath := internal.GetPicoclawHome()

			// Resolve --to from channel state if not provided
			if to == "" {
				resolved, err := resolveLastUserID(homePath, internal.GetConfigPath(), channel)
				if err != nil || resolved == "" {
					return fmt.Errorf("--to not specified and could not find last user ID for channel %q: %v", channel, err)
				}
				to = resolved
				fmt.Printf("ℹ Using last known recipient: %s\n", to)
			}

			addr, err := resolveGatewaySendURL(
				homePath,
				internal.GetConfigPath(),
				&http.Client{Timeout: 2 * time.Second},
			)
			if err != nil {
				return err
			}

			body, _ := json.Marshal(map[string]string{
				"channel": channel,
				"to":      to,
				"content": resolvedMessage,
			})

			client := &http.Client{Timeout: 15 * time.Second}
			resp, err := client.Post(addr, "application/json", bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("failed to reach gateway at %s: %w", addr, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				rawBody, _ := io.ReadAll(resp.Body)
				var result map[string]string
				if err := json.Unmarshal(rawBody, &result); err == nil && result["error"] != "" {
					return fmt.Errorf("gateway returned error: %s", result["error"])
				}
				if len(bytes.TrimSpace(rawBody)) > 0 {
					return fmt.Errorf("gateway returned status %d: %s", resp.StatusCode, bytes.TrimSpace(rawBody))
				}
				return fmt.Errorf("gateway returned status %d", resp.StatusCode)
			}

			fmt.Printf("✓ Message sent to %s:%s\n", channel, to)
			return nil
		},
	}

	cmd.Flags().StringVarP(&message, "message", "m", "", "Message content to send")
	cmd.Flags().StringVar(&messageFile, "message-file", "", "Read message content from a text file")
	cmd.Flags().StringVar(&channel, "channel", "", "Target channel (e.g. weixin, telegram)")
	cmd.Flags().StringVar(&to, "to", "", "Recipient user ID (optional, defaults to last known user)")
	_ = cmd.MarkFlagRequired("channel")

	return cmd
}

func resolveMessageContent(message, messageFile string) (string, error) {
	switch {
	case message != "" && messageFile != "":
		return "", fmt.Errorf("--message (-m) and --message-file cannot be used together")
	case messageFile != "":
		data, err := os.ReadFile(messageFile)
		if err != nil {
			return "", fmt.Errorf("reading --message-file %q: %w", messageFile, err)
		}
		if len(data) == 0 {
			return "", fmt.Errorf("--message-file %q is empty", messageFile)
		}
		return string(data), nil
	case message != "":
		return message, nil
	default:
		return "", fmt.Errorf("either --message (-m) or --message-file is required")
	}
}

// resolveLastUserID reads the channel's context-tokens state directory and
// returns the most recently active user ID when the channel persists it.
func resolveLastUserID(homePath, configPath, channel string) (string, error) {
	if strings.EqualFold(channel, "feishu") {
		if resolved, err := resolveLastFeishuChatID(homePath); err == nil && resolved != "" {
			return resolved, nil
		}
	}

	if resolved, handled, err := resolveLastUserIDFromContextState(homePath, channel); handled {
		return resolved, err
	}
	return resolveLastUserIDFromSessions(configPath, channel)
}

func resolveLastFeishuChatID(homePath string) (string, error) {
	dir := filepath.Join(homePath, "channels", "feishu", "direct-chats")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("reading direct chat state dir %s: %w", dir, err)
		}
		return "", fmt.Errorf("reading direct chat state dir %s: %w", dir, err)
	}

	var latestFile string
	var latestTime time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latestFile = filepath.Join(dir, e.Name())
		}
	}

	if latestFile == "" {
		return "", fmt.Errorf("no direct chat state files found in %s", dir)
	}

	data, err := os.ReadFile(latestFile)
	if err != nil {
		return "", err
	}

	var state directChatsState
	if err := json.Unmarshal(data, &state); err != nil {
		return "", fmt.Errorf("parsing direct chat state file: %w", err)
	}

	if state.LastUserID != "" {
		if chatID := strings.TrimSpace(state.Chats[state.LastUserID]); chatID != "" {
			return chatID, nil
		}
	}

	switch len(state.Chats) {
	case 0:
		return "", fmt.Errorf("no direct chat IDs found in state file")
	case 1:
		for _, chatID := range state.Chats {
			if strings.TrimSpace(chatID) != "" {
				return chatID, nil
			}
		}
	}

	return "", errors.New("direct chat state contains multiple users but no persisted last_user_id; specify --to explicitly")
}

func resolveLastUserIDFromContextState(homePath, channel string) (string, bool, error) {
	dir := filepath.Join(homePath, "channels", channel, "context-tokens")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", true, fmt.Errorf("reading state dir %s: %w", dir, err)
	}

	// Find the most recently modified file
	var latestFile string
	var latestTime time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latestFile = filepath.Join(dir, e.Name())
		}
	}

	if latestFile == "" {
		return "", false, nil
	}

	data, err := os.ReadFile(latestFile)
	if err != nil {
		return "", true, err
	}

	var state contextTokensState
	if err := json.Unmarshal(data, &state); err != nil {
		return "", true, fmt.Errorf("parsing state file: %w", err)
	}

	if state.LastUserID != "" {
		if len(state.Tokens) == 0 || state.Tokens[state.LastUserID] != "" {
			return state.LastUserID, true, nil
		}
	}

	switch len(state.Tokens) {
	case 0:
		return "", false, nil
	case 1:
		for userID := range state.Tokens {
			return userID, true, nil
		}
	}

	return "", true, errors.New("state file contains multiple user IDs but no persisted last_user_id; specify --to explicitly")
}

func resolveLastUserIDFromSessions(configPath, channel string) (string, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return "", fmt.Errorf("loading config for session fallback: %w", err)
	}

	sessionsDir := filepath.Join(cfg.WorkspacePath(), "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return "", fmt.Errorf("reading sessions dir %s: %w", sessionsDir, err)
	}

	var latestPath string
	var latestTime time.Time
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".meta.json") {
			continue
		}
		path := filepath.Join(sessionsDir, entry.Name())
		peerID, ok, err := sessionPeerIDForChannel(path, channel)
		if err != nil || !ok || peerID == "" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latestPath = path
		}
	}

	if latestPath == "" {
		return "", fmt.Errorf("no recent direct session found for channel %q in %s", channel, sessionsDir)
	}

	peerID, ok, err := sessionPeerIDForChannel(latestPath, channel)
	if err != nil {
		return "", err
	}
	if !ok || peerID == "" {
		return "", fmt.Errorf("latest session %s does not contain a direct peer for channel %q", latestPath, channel)
	}
	return peerID, nil
}

func sessionPeerIDForChannel(metaPath, channel string) (string, bool, error) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return "", false, err
	}

	var meta struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", false, fmt.Errorf("parsing session meta %s: %w", metaPath, err)
	}

	parts := strings.Split(meta.Key, ":")
	if len(parts) < 4 || parts[0] != "agent" {
		return "", false, nil
	}

	if len(parts) >= 5 && strings.EqualFold(parts[2], channel) && parts[3] == "direct" {
		return strings.Join(parts[4:], ":"), true, nil
	}
	if len(parts) >= 6 && strings.EqualFold(parts[2], channel) && parts[4] == "direct" {
		return strings.Join(parts[5:], ":"), true, nil
	}

	return "", false, nil
}

func resolveGatewaySendURL(homePath, configPath string, probeClient *http.Client) (string, error) {
	if pidData := pid.ReadPidFileWithCheck(homePath); pidData != nil {
		return fmt.Sprintf(
			"http://%s:%d/v1/send",
			normalizeGatewayHost(pidData.Host),
			pidData.Port,
		), nil
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return "", fmt.Errorf("gateway PID file missing and config load failed: %w", err)
	}
	if cfg.Gateway.Port <= 0 {
		return "", fmt.Errorf("gateway PID file missing and configured gateway port is invalid: %d", cfg.Gateway.Port)
	}

	baseURL := fmt.Sprintf("http://%s:%d", normalizeGatewayHost(cfg.Gateway.Host), cfg.Gateway.Port)
	if err := probeGatewayHealth(baseURL, probeClient); err != nil {
		return "", fmt.Errorf("gateway PID file missing and configured gateway %s is unreachable: %w", baseURL, err)
	}

	return baseURL + "/v1/send", nil
}

func normalizeGatewayHost(host string) string {
	if host == "" || host == "0.0.0.0" {
		return "127.0.0.1"
	}
	return host
}

func probeGatewayHealth(baseURL string, client *http.Client) error {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}

	req, err := http.NewRequest(http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}
	return nil
}

func parseHostPort(rawURL string) (string, int, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", 0, err
	}

	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return "", 0, err
	}

	return host, port, nil
}
