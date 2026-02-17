# Install

Install script now manages binary install/upgrade/uninstall only.
Service management is done by `chatcode service ...` commands.

## Quick Install

```bash
curl -fsSL https://raw.githubusercontent.com/OpenWiseLabs/chatcode/main/scripts/install.sh | sudo bash
```

## Install Specific Version

```bash
curl -fsSL https://raw.githubusercontent.com/OpenWiseLabs/chatcode/main/scripts/install.sh | sudo bash -s -- --version v1.0.0
```

## Upgrade

```bash
curl -fsSL https://raw.githubusercontent.com/OpenWiseLabs/chatcode/main/scripts/install.sh | sudo bash -s -- --upgrade
```

## Uninstall Binary

```bash
curl -fsSL https://raw.githubusercontent.com/OpenWiseLabs/chatcode/main/scripts/install.sh | sudo bash -s -- --uninstall
```

## Configure And Run

```bash
chatcode setup
chatcode service install
chatcode service start
chatcode status
```

## Paths

- Binary: `/usr/local/bin/chatcode`
- Config: `~/.chatcode/config.yaml` (auto-expanded)
