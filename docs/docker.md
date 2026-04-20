# Docker Workflows

For deployment strategies (DinD and Bare Metal), see [deployment.md](deployment.md).

## Build and Run

```bash
docker compose up -d --build
```

The container exposes port 8080 (configurable via `PORT` in `.env`) and persists data in named volumes for the SQLite database and attachments.

## Running CLI Commands

```bash
docker compose exec whiteagent ./whiteagent <command>
```

See the [CLI reference](cli.md) for all available commands.

## Common Workflows

### Create a tenant and agent

```bash
# Generate a tenant invite code
docker compose exec whiteagent ./whiteagent invite create --type tenant

# After a user joins via the invite, list tenants to get the tenant ID
docker compose exec whiteagent ./whiteagent tenant list

# Create an agent for the tenant
docker compose exec whiteagent ./whiteagent agent create --tenant <tenant-id> --name "My Agent"

# Set agent instructions
docker compose exec whiteagent ./whiteagent agent update --tenant <tenant-id> --agent <agent-id> --instructions "You are a helpful assistant."
```

### Manage users

```bash
# Generate a user invite code for a tenant
docker compose exec whiteagent ./whiteagent invite create --type user --tenant <tenant-id>

# List users in a tenant
docker compose exec whiteagent ./whiteagent user list --tenant <tenant-id>

# Remove a user
docker compose exec whiteagent ./whiteagent user remove <user-id> --tenant <tenant-id>
```

### Enable autojoin for a tenant

By default, unknown users must provide an invite code to join. To allow anyone who messages the bot to auto-join:

```bash
# Allow anyone to join without an invite code
docker compose exec whiteagent ./whiteagent tenant update <tenant-id> --join-policy open

# Require invite codes (default)
docker compose exec whiteagent ./whiteagent tenant update <tenant-id> --join-policy invite_required

# Block all new users
docker compose exec whiteagent ./whiteagent tenant update <tenant-id> --join-policy closed

# Optionally set a rejection message for closed/invite_required tenants
docker compose exec whiteagent ./whiteagent tenant update <tenant-id> --join-policy closed --rejection-message "This workspace is not accepting new members."
```

### View and manage invite codes

```bash
# List all active invite codes
docker compose exec whiteagent ./whiteagent invite list

# Revoke a code
docker compose exec whiteagent ./whiteagent invite revoke <code>
```
