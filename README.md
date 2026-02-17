# ChatCode (Go)

ChatCode connects Telegram Bot and WhatsApp Web bridge messages to local CLI executors (`codex`, `claude`) with session isolation, streaming logs, and SQLite persistence.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/OpenWiseLabs/chatcode/main/scripts/install.sh | sudo bash
```

After install:

```bash
chatcode setup
chatcode service install
chatcode service start
chatcode status
```

## Features

- Session key: `platform + chat_id (+ thread_id)`
- Queue model: per-session serial, cross-session parallel
- Unified executor interface for Codex/Claude CLI
- Codex is started with `--full-auto`
- Streaming logs with 300-500ms batch flush
- SQLite persistence for sessions, jobs, and stream events
- Security policy with project-root constraints

## CLI

- `chatcode setup` create/update config at `~/.chatcode/config.yaml`
- `chatcode daemon` run daemon in foreground
- `chatcode status` show version, config path, service status
- `chatcode service install|start|stop|restart|status|uninstall`

`chatcode daemon` is foreground mode. Use `chatcode service ...` for background management.

## Chat Commands

- `/new <workdir>`
- `/cd [workdir]`
- `/list`
- `/codex` or `/claude`
- `/codex <prompt...>` or `/claude <prompt...>`
- `/status`
- `/reset`
- `/stop <job_id>`
- plain text message executes with current session settings

## Release

- Release workflow: `docs/RELEASE.md`
- Install/upgrade/uninstall binary: `docs/INSTALL.md`
