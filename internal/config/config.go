package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Server   ServerConfig
	Telegram TelegramConfig
	WhatsApp WhatsAppConfig
	Executor ExecutorConfig
	Queue    QueueConfig
	Stream   StreamConfig
	Security SecurityConfig
	Storage  StorageConfig
}

type ServerConfig struct {
	ListenAddr string
	Timezone   string
}

type TelegramConfig struct {
	BotToken      string
	AllowedUserID string
	Enabled       bool
}

type WhatsAppConfig struct {
	BridgeListenAddr string
	AllowedSenderID  string
	Enabled          bool
}

type ExecutorConfig struct {
	CodexBinary  string
	ClaudeBinary string
	Timeout      time.Duration
}

type QueueConfig struct {
	MaxConcurrentSessions int
	PerSessionBuffer      int
}

type StreamConfig struct {
	BatchInterval time.Duration
	MaxChunkBytes int
}

type SecurityConfig struct {
	AllowlistCommands []string
	ProjectRoot       string
	AllowedWorkdirs   []string
}

type StorageConfig struct {
	SQLitePath       string
	SessionRetention time.Duration
}

func Default() Config {
	return Config{
		Server:   ServerConfig{ListenAddr: ":8080", Timezone: "UTC"},
		Telegram: TelegramConfig{Enabled: false},
		WhatsApp: WhatsAppConfig{BridgeListenAddr: ":8090", Enabled: false},
		Executor: ExecutorConfig{
			CodexBinary:  "codex",
			ClaudeBinary: "claude",
			Timeout:      30 * time.Minute,
		},
		Queue:    QueueConfig{MaxConcurrentSessions: 8, PerSessionBuffer: 64},
		Stream:   StreamConfig{BatchInterval: 400 * time.Millisecond, MaxChunkBytes: 3500},
		Security: SecurityConfig{},
		Storage:  StorageConfig{SQLitePath: "chatcode.db", SessionRetention: 7 * 24 * time.Hour},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		if err := loadYAMLLike(path, &cfg); err != nil {
			return Config{}, err
		}
	}
	overrideEnv(&cfg)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.Telegram.Enabled && c.Telegram.BotToken == "" {
		return errors.New("telegram.bot_token is required when telegram.enabled=true")
	}
	if c.Storage.SQLitePath == "" {
		return errors.New("storage.sqlite_path is required")
	}
	if c.Stream.BatchInterval < 300*time.Millisecond || c.Stream.BatchInterval > 500*time.Millisecond {
		return fmt.Errorf("stream.batch_interval must be between 300ms and 500ms: got %s", c.Stream.BatchInterval)
	}
	if c.Queue.MaxConcurrentSessions <= 0 {
		return errors.New("queue.max_concurrent_sessions must be > 0")
	}
	if c.Queue.PerSessionBuffer <= 0 {
		return errors.New("queue.per_session_buffer must be > 0")
	}
	if len(c.Security.AllowlistCommands) == 0 {
		return errors.New("security.allowlist_commands cannot be empty")
	}
	if c.Security.ProjectRoot == "" {
		return errors.New("security.project_root cannot be empty")
	}
	return nil
}

func loadYAMLLike(path string, cfg *Config) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	section := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasSuffix(line, ":") && !strings.Contains(line, " ") {
			section = strings.TrimSuffix(line, ":")
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		if err := applyKV(cfg, section, key, val); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan config: %w", err)
	}
	return nil
}

func applyKV(cfg *Config, section, key, val string) error {
	switch section + "." + key {
	case "server.listen_addr":
		cfg.Server.ListenAddr = val
	case "server.timezone":
		cfg.Server.Timezone = val
	case "telegram.enabled":
		cfg.Telegram.Enabled = val == "true"
	case "telegram.bot_token":
		cfg.Telegram.BotToken = val
	case "telegram.allowed_user_id":
		cfg.Telegram.AllowedUserID = val
	case "whatsapp.enabled":
		cfg.WhatsApp.Enabled = val == "true"
	case "whatsapp.bridge_listen_addr":
		cfg.WhatsApp.BridgeListenAddr = val
	case "whatsapp.allowed_sender_id":
		cfg.WhatsApp.AllowedSenderID = val
	case "executor.codex_binary":
		cfg.Executor.CodexBinary = val
	case "executor.claude_binary":
		cfg.Executor.ClaudeBinary = val
	case "executor.timeout":
		d, err := time.ParseDuration(val)
		if err != nil {
			return fmt.Errorf("executor.timeout: %w", err)
		}
		cfg.Executor.Timeout = d
	case "queue.max_concurrent_sessions":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("queue.max_concurrent_sessions: %w", err)
		}
		cfg.Queue.MaxConcurrentSessions = n
	case "queue.per_session_buffer":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("queue.per_session_buffer: %w", err)
		}
		cfg.Queue.PerSessionBuffer = n
	case "stream.batch_interval":
		d, err := time.ParseDuration(val)
		if err != nil {
			return fmt.Errorf("stream.batch_interval: %w", err)
		}
		cfg.Stream.BatchInterval = d
	case "stream.max_chunk_bytes":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("stream.max_chunk_bytes: %w", err)
		}
		cfg.Stream.MaxChunkBytes = n
	case "security.allowlist_commands":
		cfg.Security.AllowlistCommands = splitCSV(val)
	case "security.project_root":
		cfg.Security.ProjectRoot = val
	case "security.allowed_workdirs":
		cfg.Security.AllowedWorkdirs = splitCSV(val)
	case "storage.sqlite_path":
		cfg.Storage.SQLitePath = val
	case "storage.session_retention":
		d, err := time.ParseDuration(val)
		if err != nil {
			return fmt.Errorf("storage.session_retention: %w", err)
		}
		cfg.Storage.SessionRetention = d
	}
	return nil
}

func splitCSV(v string) []string {
	items := strings.Split(v, ",")
	out := make([]string, 0, len(items))
	for _, item := range items {
		trim := strings.TrimSpace(item)
		if trim != "" {
			out = append(out, trim)
		}
	}
	return out
}

func overrideEnv(cfg *Config) {
	if v := os.Getenv("CHATBRIDGE_TELEGRAM_TOKEN"); v != "" {
		cfg.Telegram.BotToken = v
	}
	if v := os.Getenv("CHATBRIDGE_WHATSAPP_ALLOWED_SENDER"); v != "" {
		cfg.WhatsApp.AllowedSenderID = v
	}
}
