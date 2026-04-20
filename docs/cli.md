# CLI Reference

All commands accept `--config <path>` (default: `config.json`).

When running via Docker, prefix commands with:

```bash
docker compose exec whiteagent ./whiteagent <command>
```

## serve

Start the agent runtime with all plugins, channels, and HTTP gateway.

```
whiteagent serve [--config <path>]
```

## tenant

Manage tenants.

| Command | Arguments | Flags | Description |
|---------|-----------|-------|-------------|
| `tenant list` | | | List all tenants |
| `tenant delete` | `<tenant-id>` | | Soft-delete a tenant |
| `tenant update` | `<tenant-id>` | `--name`, `--instructions`, `--join-policy`, `--rejection-message` | Update tenant settings |

`--join-policy` accepts: `open`, `invite_required`, `closed`.

## agent

Manage agents within a tenant.

| Command | Flags | Description |
|---------|-------|-------------|
| `agent create` | `--tenant` (required), `--name` (required), `--instructions` | Create a new agent |
| `agent list` | `--tenant` (required) | List agents for a tenant |
| `agent view` | `--tenant` (required), `--agent` (required) | View agent details |
| `agent update` | `--tenant` (required), `--agent` (required), `--instructions`, `--add-tool`, `--remove-tool` | Update agent config |

## user

Manage users within a tenant.

| Command | Arguments | Flags | Description |
|---------|-----------|-------|-------------|
| `user list` | | `--tenant` (required) | List users in a tenant |
| `user remove` | `<user-id>` | `--tenant` (required) | Soft-delete a user |

## invite

Manage invite codes for tenant and user onboarding.

| Command | Arguments | Flags | Description |
|---------|-----------|-------|-------------|
| `invite create` | | `--type` (required: `tenant` or `user`), `--tenant` (required for `user` type), `--target` | Generate an invite code |
| `invite list` | | `--type`, `--tenant` | List invite codes with optional filters |
| `invite revoke` | `<code>` | | Revoke an invite code |

## workspace

Manage channel-to-tenant workspace mappings.

| Command | Flags | Description |
|---------|-------|-------------|
| `workspace list` | `--tenant` | List workspace mappings, optionally filtered by tenant |
| `workspace delete` | `--channel` (required), `--workspace` (required) | Delete a workspace mapping |
