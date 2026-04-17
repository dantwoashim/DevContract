# Changelog

All notable changes to DevContract will be documented in this file.

The format follows a simple Keep a Changelog style, and releases are tagged in Git.

## Unreleased

### Added
- Scoped relay principals for human members and service principals.
- Owner-transfer, relay invite administration, and relay audit visibility.
- Ancestor-aware revision metadata and peer acknowledgement tracking.
- Public architecture, threat model, self-hosting, FAQ, roadmap, and OSS scope docs.
- Repo hygiene checks, issue templates, pull request template, CODEOWNERS, and Dependabot config.

### Changed
- Relay membership, invite, and audit state now flows through the per-project durable coordinator.
- Three-way merge now preserves more of the local file structure and comments.
- README, install guidance, and public repo layout were rewritten for outside users.

### Removed
- Tracked repo-local assistant outputs, editor configs, and machine-specific project state from the public tree.
