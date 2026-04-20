# whiteagent

Plugin-driven multi-tenant AI agent runtime. Single Go binary with `.so` plugins, supporting multiple LLM backends and chat platforms.

## Features

- Multi-tenant architecture with per-tenant agents, users, and invite codes
- Plugin system for LLM backends, chat channels, tools, sandbox, and middleware
- Docker sandbox for isolated code execution with container hardening
- Built-in tools: web search, shell, memory, scheduling, file access, and more
- Chat platform adapters (Telegram, Microsoft Teams)
- CLI for tenant, agent, user, and workspace management

## Quick Start

```bash
cp config.example.json config.json
cp .env.example .env
# Edit config.json and .env with your API keys
docker compose up -d --build
```

## Documentation

| Topic | Description |
|-------|-------------|
| [CLI Reference](docs/cli.md) | Tenant, agent, user, invite, and workspace commands |
| [Configuration](docs/configuration.md) | Config sections and environment variable syntax |
| [Docker Workflows](docs/docker.md) | Running in Docker, common recipes |
| [Deployment](docs/deployment.md) | DinD, DooD, and bare metal strategies |
| [Security Model](docs/security-model.md) | Sandbox hardening and deployment tradeoffs |

## Development

Prerequisites: Go 1.24+, CGO enabled.

```bash
make build    # Build binary to bin/whiteagent
make plugins  # Build plugin .so files
make test     # Run all tests
make fmt      # Format code
make clean    # Remove bin/
```

Run `make help` for all targets.

### From Source

```bash
cp config.example.json config.json
cp .env.example .env
make build plugins
./bin/whiteagent serve --config config.json
```
