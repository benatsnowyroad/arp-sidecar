package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/benatsnowyroad/arp-sidecar/internal/client"
	"github.com/benatsnowyroad/arp-sidecar/internal/config"
)

var (
	cfgFile  string
	logLevel string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "arp-sidecar",
		Short: "ARP sidecar — bridges the Agent Relay Protocol to OpenClaw hooks",
		RunE:  run,
	}

	rootCmd.Flags().StringVarP(&cfgFile, "config", "c", "arp-sidecar.yaml", "config file path")
	rootCmd.Flags().StringVar(&logLevel, "log-level", "info", "log level (debug, info, warn, error)")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	level := parseLogLevel(logLevel)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	logger.Info("starting arp-sidecar",
		"agentId", cfg.AgentID,
		"relay", cfg.RelayURL,
		"hooks", cfg.HooksURL,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	c := client.New(cfg, logger)
	return c.Run(ctx)
}

func parseLogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
