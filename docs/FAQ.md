# FAQ

## What problem does EnvSync solve?

It makes local repository setup more reproducible and less dependent on copying `.env` files, chat messages, and tribal knowledge.

## When should I use it?

Use EnvSync when:

- a repository has shared local environment variables
- onboarding takes too long
- local setup requires a repeatable contract, health checks, and recovery
- you want CI to pull the same shared development config path safely

## When should I not use it?

Do not use EnvSync as a replacement for a production-grade secrets manager, runtime secret injection platform, or infrastructure vault.

## Does the relay see plaintext values?

No. Relay blobs are encrypted client-side for the intended recipient. The relay can still see metadata such as sender, recipient, file name, size, and upload timing.

## How does identity work?

Human users authenticate with Ed25519 SSH keys. Transport identity is derived from the same local key material for peer-to-peer sync.

## What is the difference between LAN and relay sync?

- LAN: direct peer-to-peer delivery over a secure transport.
- Relay: encrypted blob handoff through the relay when direct delivery is unavailable.

## What happens on conflicts?

EnvSync uses the local revision store and a three-way merge policy. If it cannot merge safely, it stops and asks for manual intervention instead of guessing.

## Does EnvSync keep history?

Yes. Local revisions and backups are stored encrypted at rest so you can inspect history, roll back, or recover from bad syncs.

## Does this repo ship the hosted service?

No. This repository contains the CLI, relay source, action, and extension. Running a public hosted service still requires your own deployment, operations, and support setup.
