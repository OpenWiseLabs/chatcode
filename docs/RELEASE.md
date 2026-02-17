# Release

This project supports GitHub tag-driven release automation.

## Trigger

1. Push a version tag, for example `v1.0.0`.
2. GitHub Actions workflow `.github/workflows/release.yml` starts automatically.

## Outputs

For each target:

1. `linux/amd64`
2. `linux/arm64`
3. `darwin/amd64`
4. `darwin/arm64`

The pipeline builds:

1. `chatcode_${VERSION}_${GOOS}_${GOARCH}.tar.gz`
2. `checksums.txt`

All files are uploaded to the GitHub Release for that tag.

Each package includes:

1. `chatcode` binary
2. `scripts/install.sh`
3. `configs/config.example.yaml`
4. `docs/INSTALL.md` (if present)

## Local Dry Run

Run local package command after building one binary:

```bash
mkdir -p dist
go build -o dist/chatcode-linux-amd64 ./cmd/chatcode
scripts/package.sh --version v0.0.0 --goos linux --goarch amd64 --binary dist/chatcode-linux-amd64 --out-dir dist
```

## Rollback

1. Delete the problematic GitHub Release.
2. Delete the problematic tag from remote.
3. Fix workflow or source code.
4. Create a new tag and push again.
