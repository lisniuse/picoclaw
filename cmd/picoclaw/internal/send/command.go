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
				resolved, err := resolveLastUserID(homePath, channel)
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
func resolveLastUserID(homePath, channel string) (string, error) {
	dir := filepath.Join(homePath, "channels", channel, "context-tokens")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("reading state dir %s: %w", dir, err)
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
		return "", fmt.Errorf("no state files found in %s", dir)
	}

	data, err := os.ReadFile(latestFile)
	if err != nil {
		return "", err
	}

	var state contextTokensState
	if err := json.Unmarshal(data, &state); err != nil {
		return "", fmt.Errorf("parsing state file: %w", err)
	}

	if state.LastUserID != "" {
		if len(state.Tokens) == 0 || state.Tokens[state.LastUserID] != "" {
			return state.LastUserID, nil
		}
	}

	switch len(state.Tokens) {
	case 0:
		return "", fmt.Errorf("no user IDs found in state file")
	case 1:
		for userID := range state.Tokens {
			return userID, nil
		}
	}

	return "", errors.New("state file contains multiple user IDs but no persisted last_user_id; specify --to explicitly")
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
