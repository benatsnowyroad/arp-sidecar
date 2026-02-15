package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	AgentID    string `yaml:"agentId"`
	Token      string `yaml:"token"`
	RelayURL   string `yaml:"relayUrl"`
	HooksURL   string `yaml:"hooksUrl"`
	HooksToken string `yaml:"hooksToken"`
}

// Load reads config from a YAML file, then overrides with environment variables.
func Load(path string) (*Config, error) {
	cfg := &Config{}

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading config: %w", err)
		}
		// File doesn't exist — that's fine, we'll use env vars only.
	} else {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing config: %w", err)
		}
	}

	// Environment overrides (ARP_AGENT_ID, ARP_TOKEN, etc.)
	applyEnv(&cfg.AgentID, "ARP_AGENT_ID")
	applyEnv(&cfg.Token, "ARP_TOKEN")
	applyEnv(&cfg.RelayURL, "ARP_RELAY_URL")
	applyEnv(&cfg.HooksURL, "ARP_HOOKS_URL")
	applyEnv(&cfg.HooksToken, "ARP_HOOKS_TOKEN")

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	var missing []string
	if c.AgentID == "" {
		missing = append(missing, "agentId")
	}
	if c.Token == "" {
		missing = append(missing, "token")
	}
	if c.RelayURL == "" {
		missing = append(missing, "relayUrl")
	}
	if c.HooksURL == "" {
		missing = append(missing, "hooksUrl")
	}
	if c.HooksToken == "" {
		missing = append(missing, "hooksToken")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}
	return nil
}

// WebSocketURL builds the full WS connection URL.
func (c *Config) WebSocketURL() string {
	base := strings.TrimRight(c.RelayURL, "/")
	return fmt.Sprintf("%s/ws/agent/%s?token=%s", base, c.AgentID, c.Token)
}

func applyEnv(field *string, key string) {
	if v := os.Getenv(key); v != "" {
		*field = v
	}
}
