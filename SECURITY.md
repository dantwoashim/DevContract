# Security Policy

## Reporting Vulnerabilities

Please do not open public issues for security vulnerabilities.

Use GitHub's private vulnerability reporting for this repository if it is enabled. If private reporting is not available, email the maintainer at **twodan033@gmail.com**.

The current repository is open source code, not a managed security service. Operational response times and hosted-service guarantees are not implied by the code alone.

## Cryptographic Design

| Layer | Primitive | Purpose |
| --- | --- | --- |
| Identity | Ed25519 (SSH keys) | Member identity and request signing |
| Key Exchange | X25519 | Transport and relay shared-secret derivation |
| Channel Encryption | Noise_XX + XChaCha20-Poly1305 | LAN peer-to-peer transport |
| At-Rest Encryption | XChaCha20-Poly1305 + HKDF-SHA256 | Local encrypted backups |
| Relay Encryption | Ephemeral X25519 + XChaCha20-Poly1305 | Per-recipient relay blobs |
| Request Auth | Ed25519 signatures (`ES-SIG`) | Relay API authentication |
| Key Derivation | HKDF-SHA256 | Backup and relay encryption key derivation |

DevContract relies on audited libraries from `golang.org/x/crypto` and `github.com/flynn/noise`. It does not define custom cryptographic primitives.

## Trust Model

- Identity comes from an Ed25519 SSH key and a derived X25519 transport key.
- The relay verifies the claimed identity fingerprint from the submitted public key during explicit enrollment flows and rejects mismatches.
- Unknown fingerprints cannot silently self-register on arbitrary relay routes; they must bootstrap a new project or join through a valid invite.
- Local peer state is stored in the registry with explicit trust transitions.
- Members imported from the relay are not automatically treated as fully trusted peers.
- Trust states are `unknown`, `pending`, `trusted`, and `revoked`.
- LAN pulls require a transport key match against the trusted local registry.
- Relay blobs must include a sender signature that is verified before the payload is applied.

## Zero-Knowledge Relay

The relay never stores plaintext `.env` contents.

1. Payloads are encrypted client-side before upload.
2. Each relay blob is encrypted for one intended recipient.
3. The relay stores only ciphertext plus routing metadata.
4. Blob retention is controlled by the relay retention policy configured on that deployment.

Visible relay metadata includes sender fingerprint, recipient fingerprint, filename, size, and upload timing.

## What Revocation Means

Revoking a member stops new relay deliveries and removes them from active membership.

Revocation does **not** retroactively erase secrets that member already received. If previously shared secrets must be invalidated, rotate them in the application and, when appropriate, rotate the DevContract identity or project material as well.

## What DevContract Does Not Protect Against

- A compromised SSH private key.
- A compromised local machine where plaintext values are already present.
- Relay metadata disclosure.
- Denial of service by an unavailable or malicious relay.
- Business-logic mistakes in the application that consumes the synced secrets.
