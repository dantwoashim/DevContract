# EnvSync

EnvSync is an open-source tool for reproducible local development setup, encrypted `.env` sharing, and safer repository onboarding.

It is for teams that still pass local config through chat threads, stale setup docs, copied `.env` files, and tribal knowledge. EnvSync gives the repository a setup contract, keeps sync history locally encrypted, and uses direct peer delivery with a relay fallback when needed.

## What It Does Today

- bootstrap a repository-defined local setup flow
- validate runtimes, services, contract health, and sensitive text surfaces
- invite and join trusted project members
- sync `.env` changes between trusted machines
- keep encrypted local revision and backup history
- support scoped relay pull workflows for CI or service principals

## What Is Still Experimental

- generated assistant/editor instruction files and MCP config
- some extension surfaces
- entitlement and upgrade messaging

EnvSync is for development environments. It is not a production secrets manager or a hosted control plane by itself.

## Quick Start

### Install from GitHub Releases

macOS and Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/dantwoashim/Env_sync/main/scripts/install.sh | bash
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/dantwoashim/Env_sync/main/scripts/install.ps1 | iex
```

### Build from Source

```bash
git clone https://github.com/dantwoashim/Env_sync.git
cd Env_sync
go build -o envsync ./
```

### First Run

In your own repository:

```bash
envsync init
envsync bootstrap
envsync doctor
```

Invite a teammate:

```bash
envsync invite teammate
```

Sync changes:

```bash
envsync push
envsync pull
```

## Core Commands

- `envsync init`: create local identity state and scaffold a starter contract in your repository
- `envsync bootstrap`: run the repository setup contract after trust review
- `envsync doctor`: check local prerequisites and repository health
- `envsync guard scan`: scan repo text surfaces for likely secret leaks
- `envsync invite` / `envsync join`: manage human project membership
- `envsync service-key`: register scoped relay machine identities
- `envsync push` / `envsync pull`: exchange encrypted `.env` state
- `envsync backup`, `restore`, `rollback`: inspect and recover local history

## How It Works

- Each repository can define its local setup contract in `.envsync/contract.yaml`.
- Human identity comes from an Ed25519 SSH key.
- LAN sync uses direct peer transport when a trusted peer is reachable.
- Relay sync uploads a per-recipient encrypted blob when direct delivery is unavailable.
- Revision and backup history stays local and encrypted at rest.

More detail:

- [Architecture](docs/ARCHITECTURE.md)
- [Contract Reference](docs/CONTRACT.md)
- [Threat Model](docs/THREAT_MODEL.md)
- [Self-Hosting](docs/SELF_HOSTING.md)
- [FAQ](docs/FAQ.md)
- [OSS Scope](docs/OSS_SCOPE.md)
- [Operations](docs/OPERATIONS.md)
- [Releases](docs/RELEASES.md)

## Security Summary

- request authentication uses Ed25519 signatures
- peer-to-peer sync uses a secure transport
- relay payloads are encrypted before upload
- relay metadata is still visible to the relay operator
- local machine compromise still defeats local secrecy

See [SECURITY.md](SECURITY.md) for reporting guidance and the current security model.

## Relay and Self-Hosting

The repository includes the relay source in [relay](relay). You can self-host it on Cloudflare Workers or point the CLI at your own relay deployment. The default public relay URL in the codebase is just a default endpoint, not a promise of hosted service guarantees.

## VS Code Extension

The VS Code extension lives in [extension](extension). It shells out to the CLI and is best treated as a companion interface, not a separate implementation.

## Examples

- [examples/envsync.example.toml](examples/envsync.example.toml)
- [examples/demo/.env.example](examples/demo/.env.example)
- [examples/contracts](examples/contracts)

## License

[MIT](LICENSE)
