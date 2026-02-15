package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/benatsnowyroad/arp-sidecar/internal/config"
)

// FetchMissed retrieves messages after lastMessageID for a given channel
// using the relay's REST API.
func FetchMissed(ctx context.Context, cfg *config.Config, channelID, lastMessageID string) ([]Message, error) {
	// Build the catchup URL from the relay base.
	base := strings.TrimRight(cfg.RelayURL, "/")
	// Convert wss:// to https:// and ws:// to http://
	base = strings.Replace(base, "wss://", "https://", 1)
	base = strings.Replace(base, "ws://", "http://", 1)

	url := fmt.Sprintf("%s/api/channels/%s/messages?after=%s", base, channelID, lastMessageID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building catchup request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("catchup request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("catchup returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var msgs []Message
	if err := json.NewDecoder(resp.Body).Decode(&msgs); err != nil {
		return nil, fmt.Errorf("decoding catchup response: %w", err)
	}

	return msgs, nil
}
