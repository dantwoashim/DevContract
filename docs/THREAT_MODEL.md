# Threat Model

DevContract is designed for development-time environment sharing and onboarding. It reduces accidental secret sprawl, but it does not turn an untrusted machine or repository into a safe one.

## Trust Boundaries

- Repository contract: trusted only after the user explicitly accepts it.
- Local machine: holds plaintext `.env` data before and after sync; compromise here defeats local secrecy.
- Peer identity: derived from Ed25519 public keys and stored in the local trust registry.
- Relay: transports encrypted blobs and stores metadata; it must not be treated as a plaintext secret authority.
- CI/service principals: authenticated machine actors with scoped relay permissions.

## What Is Encrypted

- LAN sync traffic: Noise-secured transport between peers.
- Relay payloads: encrypted client-side for one intended recipient before upload.
- Local revision and backup history: encrypted at rest with a derived local key.

## What Is Not Encrypted

- Local plaintext `.env` files on the machine using them.
- Relay routing metadata such as sender fingerprint, recipient fingerprint, filename, size, and timing.
- Contract content in the repository.
- Logs and shell output produced by user-defined bootstrap commands.

## Relay Assumptions

- The relay can see metadata and can deny service.
- The relay should not be able to decrypt stored payloads.
- A compromised relay can attempt replay, delay, or deletion attacks, so clients verify signatures and payload integrity before apply.

## TOFU Caveats

DevContract still uses trust-on-first-use in a few places, but the relay no longer treats any valid signed first-contact request as automatic enrollment.

- repository contracts require an explicit local trust decision
- relay membership enrollment is limited to explicit project bootstrap and invite-bound join flows
- locally imported peers from the relay can remain pending until the operator verifies them

That means the first trust decision still matters. If a user trusts the wrong repository, accepts the wrong invite, or approves the wrong identity, DevContract cannot fully undo that mistake.

## Local Machine Caveat

If an attacker controls the developer machine, DevContract cannot protect:

- the loaded `.env` file
- the user's SSH private key
- shell commands run during bootstrap or `run`
- files written by repository-defined commands

DevContract is a safer workflow for developer setup. It is not a sandbox.
