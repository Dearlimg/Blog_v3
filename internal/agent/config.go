package agent

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	APIKey          string `json:"-"`
	OwnerToken      string `json:"-"`
	BaseURL         string
	Model           string
	DataPath        string
	SearchURL       string
	MCPConfig       string
	MaxCalls        int
	MaxTokens       int
	MaxSteps        int
	Concurrency     int
	ToolConcurrency int
	RunTimeout      time.Duration
	TrustedProxies  []string
	RetentionDays   int
}

func ConfigFromEnv() (Config, error) {
	key, err := secret("DEEPSEEK_API_KEY")
	if err != nil {
		return Config{}, err
	}
	owner, err := secret("AGENT_OWNER_TOKEN")
	if err != nil {
		return Config{}, err
	}
	return Config{
		APIKey: key, OwnerToken: owner,
		BaseURL:         env("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
		Model:           env("DEEPSEEK_MODEL", "deepseek-chat"),
		DataPath:        env("AGENT_DATA_PATH", filepath.Join(".agent-data", "agent.db")),
		SearchURL:       env("AGENT_SEARCH_URL", "https://www.bing.com/search"),
		MCPConfig:       os.Getenv("AGENT_MCP_CONFIG"),
		MaxCalls:        envInt("AGENT_MAX_CALLS", 24, 2, 100),
		MaxTokens:       envInt("AGENT_MAX_TOKENS", 80000, 4000, 200000),
		MaxSteps:        envInt("AGENT_MAX_STEPS", 12, 1, 40),
		Concurrency:     envInt("AGENT_CONCURRENCY", 3, 1, 8),
		ToolConcurrency: envInt("AGENT_TOOL_CONCURRENCY", 3, 1, 8),
		RunTimeout:      time.Duration(envInt("AGENT_TIMEOUT_SECONDS", 240, 30, 600)) * time.Second,
		TrustedProxies:  strings.FieldsFunc(os.Getenv("TRUSTED_PROXIES"), func(r rune) bool { return r == ',' }),
		RetentionDays:   envInt("AGENT_RETENTION_DAYS", 90, 1, 3650),
	}, nil
}

func secret(name string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value, nil
	}
	file := os.Getenv(name + "_FILE")
	if file == "" {
		return "", nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "", errors.New("cannot read " + name + "_FILE")
	}
	value := strings.TrimSpace(string(data))
	if value == "" || len(value) > 4096 {
		return "", errors.New("invalid secret file for " + name)
	}
	return value, nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback, minimum, maximum int) int {
	n, err := strconv.Atoi(os.Getenv(name))
	if err != nil || n < minimum || n > maximum {
		return fallback
	}
	return n
}

var keyPattern = regexp.MustCompile(`(?i)sk-[a-z0-9_-]{12,}`)

func (c Config) redact(value string) string {
	for _, key := range []string{c.APIKey, c.OwnerToken} {
		if key != "" {
			value = strings.ReplaceAll(value, key, "[REDACTED]")
		}
	}
	return keyPattern.ReplaceAllString(value, "[REDACTED]")
}
