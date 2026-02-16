package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/benatsnowyroad/arp-sidecar/internal/config"
)

// Payload is the body sent to the OpenClaw hooks endpoint.
type Payload struct {
	Message    string `json:"message"`
	SessionKey string `json:"sessionKey"`
	Name       string `json:"name,omitempty"`
	Deliver    bool   `json:"deliver"`
}

// HookResponse is the response from OpenClaw hooks endpoint.
type HookResponse struct {
	OK        bool   `json:"ok"`
	RunID     string `json:"runId"`
	SessionID string `json:"sessionId"`
}

// SessionMessage represents a message from session history.
type SessionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// SessionHistoryResponse is the response from session history endpoint.
type SessionHistoryResponse struct {
	OK       bool             `json:"ok"`
	Messages []SessionMessage `json:"messages"`
}

// Forwarder sends event payloads to the OpenClaw hooks endpoint.
type Forwarder struct {
	hooksURL   string
	hooksToken string
	client     *http.Client
	logger     *slog.Logger
}

func New(cfg *config.Config, logger *slog.Logger) *Forwarder {
	return &Forwarder{
		hooksURL:   cfg.HooksURL,
		hooksToken: cfg.HooksToken,
		client:     &http.Client{Timeout: 30 * time.Second},
		logger:     logger,
	}
}

// Send POSTs a payload to the hooks endpoint and returns the runId.
func (f *Forwarder) Send(ctx context.Context, p *Payload) (*HookResponse, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshaling payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.hooksURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.hooksToken)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending hook: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("hook returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var hookResp HookResponse
	if err := json.Unmarshal(respBody, &hookResp); err != nil {
		f.logger.Warn("could not parse hook response", "body", string(respBody), "error", err)
		// Still return success if we got 2xx
		return &HookResponse{OK: true}, nil
	}

	f.logger.Debug("hook delivered", "name", p.Name, "sessionKey", p.SessionKey, "status", resp.StatusCode, "runId", hookResp.RunID)
	return &hookResp, nil
}

// PollSessionForResponse polls the session history until we get an assistant response.
// It returns the assistant's message content.
func (f *Forwarder) PollSessionForResponse(ctx context.Context, sessionKey string, maxWait time.Duration) (string, error) {
	// Build the session history URL
	// OpenClaw session history: GET /sessions/{sessionKey}/history
	baseURL := strings.TrimSuffix(f.hooksURL, "/hooks/agent")
	baseURL = strings.TrimSuffix(baseURL, "/hooks")
	historyURL := fmt.Sprintf("%s/api/sessions/%s/history?limit=5", baseURL, sessionKey)

	deadline := time.Now().Add(maxWait)
	pollInterval := 2 * time.Second
	lastMessageCount := 0

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(pollInterval):
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, historyURL, nil)
		if err != nil {
			return "", fmt.Errorf("building history request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+f.hooksToken)

		resp, err := f.client.Do(req)
		if err != nil {
			f.logger.Warn("polling session failed", "error", err)
			continue
		}

		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 32768))
		resp.Body.Close()

		if resp.StatusCode != 200 {
			f.logger.Debug("session history not ready", "status", resp.StatusCode)
			continue
		}

		var historyResp SessionHistoryResponse
		if err := json.Unmarshal(respBody, &historyResp); err != nil {
			f.logger.Warn("could not parse history response", "error", err)
			continue
		}

		// Look for new assistant message
		if len(historyResp.Messages) > lastMessageCount {
			for i := len(historyResp.Messages) - 1; i >= 0; i-- {
				msg := historyResp.Messages[i]
				if msg.Role == "assistant" && msg.Content != "" {
					f.logger.Info("got assistant response", "length", len(msg.Content))
					return msg.Content, nil
				}
			}
			lastMessageCount = len(historyResp.Messages)
		}

		// Increase poll interval gradually
		if pollInterval < 5*time.Second {
			pollInterval += 500 * time.Millisecond
		}
	}

	return "", fmt.Errorf("timeout waiting for assistant response after %v", maxWait)
}
