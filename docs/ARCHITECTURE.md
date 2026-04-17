# Architecture

DevContract is a repository-centric developer environment tool. The repo contains four main product surfaces:

- `cmd/`: the CLI entrypoints and user-facing workflows.
- `internal/`: the Go packages that implement contracts, identity, sync, merge, storage, backups, audit, and transport.
- `relay/`: a Cloudflare Worker relay used when direct LAN delivery is not available.
- `extension/`: the VS Code extension for bootstrap, doctor, guard, and status workflows.

## Repository Layout

- `action/`: the GitHub Action for CI pull workflows.
- `docs/`: public documentation and operator guidance.
- `examples/`: safe example contracts and example `.env` inputs.
- `scripts/`: install, packaging, hygiene, and repository maintenance scripts.
- `.github/workflows/`: CI, release, and security automation.

## Data Flow

### Local bootstrap

1. `devcontract init` creates local user state and a starter project contract in the caller's repository.
2. `devcontract bootstrap` reads the contract, creates allowed output files, checks runtimes and services, and runs bootstrap steps after trust review.
3. `devcontract doctor` and `devcontract guard scan` validate the local machine, repository config, and sensitive text surfaces.

### Sync

1. `devcontract push` snapshots the current `.env`, resolves revision lineage, and prepares an encrypted payload.
2. DevContract first tries trusted LAN peers over Noise-secured transport.
3. If LAN delivery is unavailable, the client uploads a per-recipient encrypted blob to the relay.
4. `devcontract pull` or the GitHub Action downloads pending relay blobs, verifies signatures, decrypts them locally, and applies the result with the configured merge policy.

### Revision and backup model

- Local history is stored in the encrypted revision store under the user's DevContract data directory.
- Each revision records parent lineage so three-way merge can find a shared ancestor after divergence.
- Per-peer acknowledgement metadata lets the sender choose a better merge base for each recipient.
- Encrypted backups are created before apply operations when backup support is enabled.

## Relay Design

The relay is intentionally narrow:

- blob payloads live in KV as encrypted per-recipient objects
- authoritative team, invite, audit, and queue state flows through a per-team Durable Object
- request authentication is Ed25519 signature based
- relay-side authorization is scope aware

The relay is not a plaintext secret manager and is not a general-purpose control plane.

## Extension Design

The VS Code extension shells out to the CLI rather than re-implementing core logic. That keeps the security and sync behavior in one place and lets the extension report what the CLI actually sees.

## Public Repo Policy

This repository intentionally does not commit:

- local `.env` files
- repo-local `.devcontract.toml`
- generated assistant/editor outputs
- package manager install artifacts
- compiled binaries or editor build output

Those files can be generated locally when needed, but the public tree stays source-only.
