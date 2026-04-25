# Demo Script: DevContract

## 30-second explanation

DevContract is a Go CLI that lets a repository describe how local setup should work, validate that setup, and support encrypted `.env` sharing for trusted development machines.

## 2-minute walkthrough

1. Show the problem statement in the README.
2. Open `.devcontract/contract.yaml` or an example contract.
3. Run or show `devcontract validate`.
4. Explain encrypted local history.
5. Show the security/threat-model docs.

## 5-minute technical walkthrough

Walk through CLI command structure, contract parsing, validation checks, encrypted local backup/history, direct sync versus relay fallback, and why the tool is scoped to development environments.

## What to show in an interview

- A before/after onboarding example
- CLI help output
- A contract file
- Security model docs
- Go tests or CI workflow

## What not to overclaim

- Do not call it a production secrets manager.
- Do not imply the relay is a trusted vault.
- Do not claim enterprise compliance readiness.

## Likely interviewer questions

### Why Go?

Go is a good fit for cross-platform CLIs, simple distribution, and strong standard tooling.

### How is this safer than sharing `.env` in chat?

It moves sharing into a scoped workflow with local encryption, project context, and explicit trust boundaries instead of ad hoc copy-paste.

### What would you harden next?

Release signing, more relay tests, stronger key lifecycle docs, and more examples from real project types.

