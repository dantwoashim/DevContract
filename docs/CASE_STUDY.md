# Case Study: DevContract

## Summary

DevContract is a Go CLI for repo-first developer onboarding, local setup validation, and encrypted local `.env` sharing for development environments.

## Problem

Teams often onboard developers through stale README steps, copied `.env` files, chat messages, and tribal knowledge. That creates slow setup, insecure sharing habits, and inconsistent local environments.

## Why this project matters

It turns local setup into something a repository can declare, validate, and repeat.

## My role

I built the CLI concept, setup contract workflow, validation surface, encrypted local history model, and documentation around development-only secret handling.

## Tech stack

- Go CLI with Cobra
- Local cryptography and encrypted history
- Relay-assisted sync workflow
- Optional VS Code/editor surfaces
- GitHub Actions for CI and release checks

## Architecture

```text
Repository contract -> DevContract CLI -> checks/bootstrap/env workflows
                                   -> encrypted local backup/history
                                   -> direct trusted sync or constrained relay fallback
```

## Key features

- Repository-owned setup contract
- Local validation for tools, services, and config expectations
- Encrypted `.env` sharing for trusted development machines
- Encrypted local backup/history
- Relay-assisted workflows for constrained environments

## Hard technical problems

- Making setup contracts useful without becoming a hosted secrets platform
- Communicating trust boundaries clearly
- Keeping relay workflows constrained so the relay is not treated as a trusted vault
- Designing a CLI that helps onboarding without hiding too much

## Important decisions and tradeoffs

- DevContract is for development environments, not production secret injection.
- The repository is the source of onboarding expectations.
- Local encrypted history is useful, but not a replacement for formal secret management.
- Security documentation is part of the product, not an afterthought.

## Testing and validation

The project uses Go tests, `go vet`, formatting checks, CI workflows, and release checks. Security-sensitive behavior should be reviewed carefully before wider team use.

## Security and limitations

DevContract does not remove the need for production secret managers, access policies, or incident response. It reduces unsafe local sharing patterns, but teams still need key hygiene and trust decisions.

## What I learned

- CLI UX and command design
- Local-first security tradeoffs
- Go project structure and release workflows
- Writing honest security docs for developer tools

## What I would improve with more time

- More real-world contract examples
- Stronger test fixtures around relay edge cases
- Signed release hardening
- More editor integration polish

## What this project proves to employers

DevContract proves I can build practical developer tooling in Go, reason about security boundaries, and document a tool so another developer can evaluate it.

## Resume bullets

- Built a Go CLI for repo-first developer onboarding, setup validation, and encrypted `.env` sharing across trusted local development machines.
- Modeled local setup contracts, environment validation, encrypted history, and relay-assisted sync workflows for practical developer productivity.
- Added security-focused documentation around development-only secret sharing, trust boundaries, and relay constraints.

