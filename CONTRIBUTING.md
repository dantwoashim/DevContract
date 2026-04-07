# Contributing to EnvSync

Thank you for your interest in contributing!

## Development Setup

```bash
# Clone
git clone https://github.com/dantwoashim/Env_sync.git
cd envsync

# Build
go build -o envsync .

# Test
go test ./... -race -count=1

# Lint (install golangci-lint first)
golangci-lint run ./...
```

## Code Style

- Run `gofmt` on all Go files
- Follow [Effective Go](https://go.dev/doc/effective_go) conventions
- Use `go vet ./...` before committing
- All exported functions must have doc comments

## PR Process

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/my-feature`)
3. Write tests for new functionality
4. Ensure all tests pass (`go test ./... -race`)
5. Submit a pull request with a clear description

## Test Requirements

- All new code must have corresponding tests
- Tests must pass on Linux, macOS, and Windows
- Use `t.TempDir()` for temporary files (auto-cleaned)
- Mock external services (relay, GitHub API)

## Security

If you discover a security vulnerability, please follow the process in [SECURITY.md](SECURITY.md). **Do not** open a public issue.

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
