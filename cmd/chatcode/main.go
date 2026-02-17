package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"chatcode/internal/config"
	"chatcode/internal/domain"
	"chatcode/internal/executor"
	"chatcode/internal/logging"
	"chatcode/internal/security"
	"chatcode/internal/service"
	"chatcode/internal/session"
	"chatcode/internal/store"
	"chatcode/internal/transport/telegram"
	"chatcode/internal/transport/whatsapp"

	cli "github.com/urfave/cli/v2"
)

var version = "1.0.0"

const (
	linuxUserServiceName = "chatcode.service"
	macOSServiceLabel    = "com.chatcode.daemon"
)

func main() {
	app := &cli.App{
		Name:  "chatcode",
		Usage: "ChatCode CLI",
		Commands: []*cli.Command{
			{
				Name:   "setup",
				Usage:  "Create or update config file",
				Action: runSetupCommand,
			},
			{
				Name:   "daemon",
				Usage:  "Run daemon in foreground",
				Action: runDaemonCommand,
			},
			{
				Name:   "status",
				Usage:  "Show runtime and service status",
				Action: runStatusCommand,
			},
			{
				Name:   "version",
				Usage:  "Show chatcode version",
				Action: runVersionCommand,
			},
			{
				Name:  "service",
				Usage: "Manage background service",
				Subcommands: []*cli.Command{
					{Name: "install", Usage: "Install service definition", Action: runServiceInstallCommand},
					{Name: "start", Usage: "Start service", Action: runServiceStartCommand},
					{Name: "stop", Usage: "Stop service", Action: runServiceStopCommand},
					{Name: "restart", Usage: "Restart service", Action: runServiceRestartCommand},
					{Name: "status", Usage: "Show service status", Action: runServiceStatusCommand},
					{Name: "uninstall", Usage: "Uninstall service definition", Action: runServiceUninstallCommand},
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func runSetupCommand(c *cli.Context) error {
	cfgPath, err := resolveConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Config path: %s\n", cfgPath)
	defaultDataPath := filepath.Join(filepath.Dir(cfgPath), "chatcode.db")

	telegramEnabled := promptString(reader, "telegram.enabled", "true")
	telegramToken := promptString(reader, "telegram.bot_token", "")
	telegramUser := promptString(reader, "telegram.allowed_user_id", "123456789")
	projectRoot := promptString(reader, "security.project_root", filepath.Join(userHomeDir(), "projects"))
	defaultExec := promptString(reader, "default executor (codex/claude)", "codex")
	codexBinary := promptString(reader, "executor.codex_binary", firstNonEmpty(lookPathOrEmpty("codex"), "codex"))
	claudeBinary := promptString(reader, "executor.claude_binary", firstNonEmpty(lookPathOrEmpty("claude"), "claude"))
	whatsappEnabled := promptString(reader, "whatsapp.enabled", "false")
	whatsappListen := promptString(reader, "whatsapp.bridge_listen_addr", ":8090")
	whatsappSender := promptString(reader, "whatsapp.allowed_sender_id", "your-whatsapp-id")

	allowlist := "codex,claude"
	if strings.EqualFold(defaultExec, "claude") {
		allowlist = "claude,codex"
	}

	content := fmt.Sprintf(`server:
  listen_addr: ":8080"
  timezone: "UTC"

telegram:
  enabled: %s
  bot_token: "%s"
  allowed_user_id: "%s"

whatsapp:
  enabled: %s
  bridge_listen_addr: "%s"
  allowed_sender_id: "%s"

executor:
  codex_binary: "%s"
  claude_binary: "%s"
  timeout: "30m"

queue:
  max_concurrent_sessions: 8
  per_session_buffer: 64

stream:
  batch_interval: "400ms"
  max_chunk_bytes: 3500

security:
  allowlist_commands: "%s"
  project_root: "%s"

storage:
  sqlite_path: "%s"
  session_retention: "168h"
`, telegramEnabled, escapeYAML(telegramToken), escapeYAML(telegramUser), whatsappEnabled, escapeYAML(whatsappListen), escapeYAML(whatsappSender), escapeYAML(codexBinary), escapeYAML(claudeBinary), allowlist, escapeYAML(projectRoot), escapeYAML(defaultDataPath))

	if err := os.WriteFile(cfgPath, []byte(content), 0o640); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	fmt.Printf("Config written: %s\n", cfgPath)
	return nil
}

func runDaemonCommand(c *cli.Context) error {
	cfgPath, err := resolveConfigPath()
	if err != nil {
		return err
	}
	return runDaemon(cfgPath)
}

func runStatusCommand(c *cli.Context) error {
	cfgPath, err := resolveConfigPath()
	if err != nil {
		return err
	}
	exe, _ := os.Executable()
	fmt.Printf("Version: %s\n", version)
	fmt.Printf("Config: %s\n", cfgPath)
	fmt.Printf("Config exists: %t\n", fileExists(cfgPath))
	fmt.Printf("Binary: %s\n", exe)

	info, err := serviceStatus()
	fmt.Printf("Service manager: %s\n", info.Manager)
	if err != nil {
		fmt.Printf("Service status: error (%v)\n", err)
	} else {
		fmt.Printf("Service status: %s\n", info.Status)
	}
	fmt.Printf("Service PID: %s\n", emptyAs(info.PID, "(unknown)"))
	fmt.Printf("Service started: %s\n", emptyAs(info.StartedAt, "(unknown)"))
	fmt.Printf("Service last error: %s\n", emptyAs(info.LastError, "(none)"))
	return nil
}

func runVersionCommand(c *cli.Context) error {
	fmt.Println(version)
	return nil
}

func runServiceInstallCommand(c *cli.Context) error {
	cfgPath, err := resolveConfigPath()
	if err != nil {
		return err
	}
	workingDir := filepath.Dir(cfgPath)
	servicePathEnv := defaultServicePATH()

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	switch runtime.GOOS {
	case "linux":
		unitPath, err := linuxUserUnitPath()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
			return err
		}
		unit := fmt.Sprintf(`[Unit]
Description=ChatCode User Service
After=network.target

[Service]
Type=simple
ExecStart=%s daemon
WorkingDirectory=%s
Environment=PATH=%s
Restart=always
RestartSec=2

[Install]
WantedBy=default.target
`, exe, workingDir, servicePathEnv)
		if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
			return err
		}
		if _, err := runCmd("systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		if _, err := runCmd("systemctl", "--user", "enable", linuxUserServiceName); err != nil {
			return err
		}
		fmt.Printf("Service installed: %s\n", unitPath)
	case "darwin":
		plistPath, err := macOSLaunchAgentPath()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
			return err
		}
		plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>daemon</string>
  </array>
  <key>WorkingDirectory</key>
  <string>%s</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>%s</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict>
</plist>
`, macOSServiceLabel, exe, workingDir, servicePathEnv)
		if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
			return err
		}
		fmt.Printf("Service installed: %s\n", plistPath)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
	return nil
}

func runServiceStartCommand(c *cli.Context) error {
	switch runtime.GOOS {
	case "linux":
		_, err := runCmd("systemctl", "--user", "start", linuxUserServiceName)
		return err
	case "darwin":
		plistPath, err := macOSLaunchAgentPath()
		if err != nil {
			return err
		}
		domain := fmt.Sprintf("gui/%d", os.Getuid())
		_, _ = runCmd("launchctl", "bootout", domain, plistPath)
		if _, err := runCmd("launchctl", "bootstrap", domain, plistPath); err != nil {
			return err
		}
		if _, err := runCmd("launchctl", "kickstart", "-k", domain+"/"+macOSServiceLabel); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func runServiceStopCommand(c *cli.Context) error {
	switch runtime.GOOS {
	case "linux":
		_, err := runCmd("systemctl", "--user", "stop", linuxUserServiceName)
		return err
	case "darwin":
		domain := fmt.Sprintf("gui/%d", os.Getuid())
		_, err := runCmd("launchctl", "bootout", domain+"/"+macOSServiceLabel)
		return err
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func runServiceRestartCommand(c *cli.Context) error {
	switch runtime.GOOS {
	case "linux":
		_, err := runCmd("systemctl", "--user", "restart", linuxUserServiceName)
		return err
	case "darwin":
		if err := runServiceStartCommand(c); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func runServiceStatusCommand(c *cli.Context) error {
	info, err := serviceStatus()
	fmt.Printf("Service manager: %s\n", info.Manager)
	if err != nil {
		fmt.Printf("Service status: error (%v)\n", err)
		return nil
	}
	fmt.Printf("Service status: %s\n", info.Status)
	fmt.Printf("Service PID: %s\n", emptyAs(info.PID, "(unknown)"))
	fmt.Printf("Service started: %s\n", emptyAs(info.StartedAt, "(unknown)"))
	fmt.Printf("Service last error: %s\n", emptyAs(info.LastError, "(none)"))
	return nil
}

func runServiceUninstallCommand(c *cli.Context) error {
	switch runtime.GOOS {
	case "linux":
		_, _ = runCmd("systemctl", "--user", "disable", "--now", linuxUserServiceName)
		unitPath, err := linuxUserUnitPath()
		if err != nil {
			return err
		}
		if err := os.Remove(unitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		_, err = runCmd("systemctl", "--user", "daemon-reload")
		return err
	case "darwin":
		domain := fmt.Sprintf("gui/%d", os.Getuid())
		_, _ = runCmd("launchctl", "bootout", domain+"/"+macOSServiceLabel)
		plistPath, err := macOSLaunchAgentPath()
		if err != nil {
			return err
		}
		if err := os.Remove(plistPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func runDaemon(cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	logger := logging.New()
	slog.SetDefault(logger)
	logger.Info("chatcode starting", "config", cfgPath)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	st, err := store.NewSQLiteStore(cfg.Storage.SQLitePath)
	if err != nil {
		return err
	}
	defer st.Close()

	sm := session.NewManager(st, cfg.Storage.SessionRetention)
	policy := security.New(cfg.Security.AllowlistCommands, []string{cfg.Security.ProjectRoot})

	execs := map[string]executor.Executor{
		"codex":  executor.CodexExecutor{Binary: cfg.Executor.CodexBinary, SessionStore: st},
		"claude": executor.ClaudeExecutor{Binary: cfg.Executor.ClaudeBinary, SessionStore: st},
	}
	transports := make(map[domain.Platform]domain.Transport)
	if cfg.Telegram.Enabled {
		transports[domain.PlatformTelegram] = telegram.New(cfg.Telegram.BotToken, cfg.Telegram.AllowedUserID)
		logger.Info("transport registered", "transport", "telegram")
	}
	if cfg.WhatsApp.Enabled {
		transports[domain.PlatformWhatsApp] = whatsapp.NewWebBridge(cfg.WhatsApp.BridgeListenAddr, cfg.WhatsApp.AllowedSenderID)
		logger.Info("transport registered", "transport", "whatsapp", "listen_addr", cfg.WhatsApp.BridgeListenAddr)
	}

	orch := service.NewOrchestrator(
		ctx,
		st,
		sm,
		policy,
		executor.Runner{Timeout: cfg.Executor.Timeout},
		execs,
		transports,
		cfg.Queue.MaxConcurrentSessions,
		cfg.Queue.PerSessionBuffer,
		cfg.Stream.BatchInterval,
		cfg.Stream.MaxChunkBytes,
	)

	for _, t := range transports {
		go func(tp domain.Transport) {
			logger.Info("transport starting", "transport", tp.Name())
			if err := tp.Start(ctx, orch.HandleIncomingMessage); err != nil && ctx.Err() == nil {
				logger.Error("transport stopped with error", "transport", tp.Name(), "error", err)
			}
		}(t)
	}

	<-ctx.Done()
	logger.Info("chatcode stopped")
	return nil
}

func resolveConfigPath() (string, error) {
	return expandHome("~/.chatcode/config.yaml")
}

func expandHome(path string) (string, error) {
	if path == "~" {
		return userHomeDir(), nil
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(userHomeDir(), path[2:]), nil
	}
	return path, nil
}

func userHomeDir() string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return "."
	}
	return h
}

func promptString(r *bufio.Reader, label, defaultVal string) string {
	if defaultVal == "" {
		fmt.Printf("%s: ", label)
	} else {
		fmt.Printf("%s [%s]: ", label, defaultVal)
	}
	line, err := r.ReadString('\n')
	if err != nil {
		return defaultVal
	}
	v := strings.TrimSpace(line)
	if v == "" {
		return defaultVal
	}
	return v
}

func escapeYAML(v string) string {
	return strings.ReplaceAll(v, "\"", "\\\"")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func linuxUserUnitPath() (string, error) {
	h := userHomeDir()
	if h == "." {
		return "", errors.New("cannot resolve home directory")
	}
	return filepath.Join(h, ".config", "systemd", "user", linuxUserServiceName), nil
}

func macOSLaunchAgentPath() (string, error) {
	h := userHomeDir()
	if h == "." {
		return "", errors.New("cannot resolve home directory")
	}
	return filepath.Join(h, "Library", "LaunchAgents", macOSServiceLabel+".plist"), nil
}

func runCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	o := strings.TrimSpace(string(out))
	if err != nil {
		if o == "" {
			return "", err
		}
		return o, fmt.Errorf("%s %v: %w (%s)", name, args, err, o)
	}
	return o, nil
}

type serviceStatusInfo struct {
	Manager   string
	Status    string
	PID       string
	StartedAt string
	LastError string
}

func serviceStatus() (serviceStatusInfo, error) {
	switch runtime.GOOS {
	case "linux":
		info := serviceStatusInfo{Manager: "systemd --user"}
		out, e := runCmd("systemctl", "--user", "show", linuxUserServiceName, "--property=LoadState,ActiveState,MainPID,ExecMainStartTimestamp,Result,ExecMainStatus", "--no-page")
		if e != nil {
			if strings.Contains(e.Error(), "could not be found") || strings.Contains(e.Error(), "not-found") || strings.Contains(e.Error(), "loaded: not-found") {
				info.Status = "not-installed"
				return info, nil
			}
			return info, e
		}
		props := parseProperties(out)
		info.Status = props["ActiveState"]
		if info.Status == "" {
			info.Status = "unknown"
		}
		if pid := props["MainPID"]; pid != "" && pid != "0" {
			info.PID = pid
		}
		if started := props["ExecMainStartTimestamp"]; started != "" && started != "n/a" {
			info.StartedAt = started
		}
		lastErr := []string{}
		if result := props["Result"]; result != "" && result != "success" {
			lastErr = append(lastErr, "Result="+result)
		}
		if code := props["ExecMainStatus"]; code != "" && code != "0" {
			lastErr = append(lastErr, "ExitCode="+code)
		}
		if len(lastErr) > 0 {
			info.LastError = strings.Join(lastErr, ", ")
		}
		return info, nil
	case "darwin":
		info := serviceStatusInfo{Manager: "launchd (user)"}
		domain := fmt.Sprintf("gui/%d/%s", os.Getuid(), macOSServiceLabel)
		out, e := runCmd("launchctl", "print", domain)
		if e != nil {
			if strings.Contains(e.Error(), "Could not find service") {
				info.Status = "not-installed"
				return info, nil
			}
			return info, e
		}
		info.Status = parseLaunchctlValue(out, "state")
		if info.Status == "" {
			info.Status = "unknown"
		}
		info.PID = parseLaunchctlValue(out, "pid")
		info.StartedAt = parseLaunchctlValue(out, "since")
		lastExit := parseLaunchctlValue(out, "last exit code")
		if lastExit != "" && lastExit != "0" {
			info.LastError = "LastExitCode=" + lastExit
		}
		return info, nil
	default:
		return serviceStatusInfo{Manager: "unknown", Status: "unsupported"}, nil
	}
}

func parseProperties(out string) map[string]string {
	props := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		props[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return props
}

func parseLaunchctlValue(out, key string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		prefix := key + " = "
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func emptyAs(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func lookPathOrEmpty(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func defaultServicePATH() string {
	parts := []string{
		"/usr/local/bin",
		"/opt/homebrew/bin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	}
	if cur := strings.TrimSpace(os.Getenv("PATH")); cur != "" {
		parts = append([]string{cur}, parts...)
	}
	return strings.Join(uniqueStrings(parts), ":")
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
