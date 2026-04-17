# Contributing to EnvSync

Thanks for contributing.

## Development Setup

Recommended:

- open the repo in the included devcontainer
- run `make verify`

Supported local toolchain outside the devcontainer:

- Go `1.25.8`
- Node.js `22.x`
- npm `10+`
- GNU `make`
- Bash-compatible shell for repo hygiene and packaging scripts

```bash
git clone https://github.com/dantwoashim/Env_sync.git
cd Env_sync

go build -o envsync ./
go test ./...
```

Relay and extension work use their own package managers:

```bash
cd relay && npm ci && npm test
cd ../extension && npm ci && npm test
```

Canonical full-suite verification:

```bash
make verify
```

## Code Style

- keep changes small and reviewable
- add or update tests when behavior changes
- run `gofmt` on Go changes
- run `go vet ./...` for Go changes
- run `scripts/check_repo_hygiene.sh` before sending a cleanup or packaging PR

## Public Repo Hygiene

Do not commit:

- local `.env` files
- `.envsync.toml`
- `.envsync/`
- editor or assistant-specific generated outputs
- `node_modules`, `.wrangler`, `.gocache`, extension build output, or binaries

This repository intentionally stays source-only.

## Pull Requests

- describe the change clearly
- list the validation you ran
- call out any behavior changes or follow-up work

## Security

If you discover a security vulnerability, please follow the process in [SECURITY.md](SECURITY.md). **Do not** open a public issue.

## License

By contributing, you agree that your contributions are licensed under the [MIT License](LICENSE).
