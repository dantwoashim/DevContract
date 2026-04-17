# OSS Scope

This document describes what this repository supports today and what should still be treated as beta or experimental.

## Stable

- core CLI workflows for init, bootstrap, doctor, guard, invite, join, push, pull, backup, restore, rollback, and status
- encrypted local revision and backup history
- scoped service principal registration for relay pull workflows
- GitHub Action pull path
- self-hostable relay source code

## Beta

- relay administration surfaces for invites, audit history, and ownership workflows
- VS Code extension health and workflow surfaces
- cross-device merge behavior in more complex long-running histories

## Experimental

- generated assistant/editor instruction files and MCP config
- relay limits messaging
- any hosted billing story

## Out of Scope

- production runtime secret injection
- a full enterprise secrets manager
- managed SaaS operations, support, or compliance guarantees from the repository alone
