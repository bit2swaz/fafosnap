package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// Config captures backend configuration parameters loaded from JSON/env.
type Config struct {
	ServerPort        int      `json:"serverPort"`
	WorkerCount       int      `json:"workerCount"`
	JobTimeoutSeconds int      `json:"jobTimeoutSeconds"`
	MaxUrlsPerRequest int      `json:"maxUrlsPerRequest"`
	MaxAttempts       int      `json:"maxAttempts"`
	DefaultURLs       []string `json:"defaultUrls"`
}

// LoadConfiguration reads configuration from disk and overrides via environment.
func LoadConfiguration(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := overrideWithEnv(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func overrideWithEnv(cfg *Config) error {
	overrides := []struct {
		env   string
		apply func(string) error
	}{
		{
			env: "FAFOSNAP_SERVER_PORT",
			apply: func(val string) error {
				parsed, err := strconv.Atoi(val)
				if err != nil {
					return fmt.Errorf("invalid FAFOSNAP_SERVER_PORT: %w", err)
				}
				cfg.ServerPort = parsed
				return nil
			},
		},
		{
			env: "FAFOSNAP_WORKER_COUNT",
			apply: func(val string) error {
				parsed, err := strconv.Atoi(val)
				if err != nil {
					return fmt.Errorf("invalid FAFOSNAP_WORKER_COUNT: %w", err)
				}
				cfg.WorkerCount = parsed
				return nil
			},
		},
		{
			env: "FAFOSNAP_JOB_TIMEOUT_SECONDS",
			apply: func(val string) error {
				parsed, err := strconv.Atoi(val)
				if err != nil {
					return fmt.Errorf("invalid FAFOSNAP_JOB_TIMEOUT_SECONDS: %w", err)
				}
				cfg.JobTimeoutSeconds = parsed
				return nil
			},
		},
		{
			env: "FAFOSNAP_MAX_URLS_PER_REQUEST",
			apply: func(val string) error {
				parsed, err := strconv.Atoi(val)
				if err != nil {
					return fmt.Errorf("invalid FAFOSNAP_MAX_URLS_PER_REQUEST: %w", err)
				}
				cfg.MaxUrlsPerRequest = parsed
				return nil
			},
		},
		{
			env: "FAFOSNAP_MAX_ATTEMPTS",
			apply: func(val string) error {
				parsed, err := strconv.Atoi(val)
				if err != nil {
					return fmt.Errorf("invalid FAFOSNAP_MAX_ATTEMPTS: %w", err)
				}
				cfg.MaxAttempts = parsed
				return nil
			},
		},
	}

	for _, override := range overrides {
		if val, ok := os.LookupEnv(override.env); ok {
			if err := override.apply(val); err != nil {
				return err
			}
		}
	}

	return nil
}
