# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
make build          # Build binary to bin/whiteagent
make plugins        # Build all plugin .so files to bin/plugins/ (requires CGO_ENABLED=1)
make test           # go test ./...
make fmt            # gofmt -w
make tidy           # go mod tidy
make validate-config # Validate JSON config syntax
make run            # Run with bin/whiteagent.json config
make clean          # Remove bin/
```

Run a single test: `go test ./internal/domain/service/ -run TestName`

Plugins are built via `scripts/build-plugins.sh` which auto-generates `_build/main.go` wrappers that re-export `Manifest` and `NewPlugin` symbols as `.so` files.

## Architecture

**Module:** `github.com/whiteagent-org/whiteagent` (Go 1.24)

Plugin-driven multi-tenant AI agent runtime. Single binary (`cmd/whiteagent/`) loads `.so` plugins at startup.

### Plugin System (core abstraction)

Six plugin kinds, initialized in fixed order: Store → Transport → Channel → LLM → Tool → Middleware (stopped in reverse).

All plugins implement `port.Plugin` (ID, Kind, Init, Start, Stop, Status). Kind-specific interfaces in `internal/domain/port/`:
- **Transport** — topic-based pub/sub (singleton)
- **Channel** — external platform adapter (Telegram, etc.)
- **LLM** — model API completions; configured as drivers with named endpoints
- **Tool** — LLM tool execution (8 built-in tools in `internal/infra/plugins/tool/`)
- **Store** — persistence (singleton, SQLite)
- **Middleware** — message handler wrapping pipeline

Optional dependency injection interfaces: `StoreAware`, `TransportAware`, `MiddlewareAware`, `ConversationAware`, `ScopedFSAware`.

### Message Flow

1. Channel receives external message → `dto.IncomingMessage`
2. Runtime's identity resolver enriches to `entity.Message`
3. Transport publishes to `TopicInbound`
4. Agent loop (ReAct cycle) subscribes, runs LLM + tools up to MaxIterations
5. Response published to `TopicOutbound` → routed back to channel

### Key Packages

- `internal/app/agent/runtime.go` — plugin lifecycle, wiring, dependency injection (no DI framework)
- `internal/app/agent/agent.go` — ReAct agent loop with per-session mutex, bounded concurrency (semaphore), per-turn timeout
- `internal/app/gateway/` — HTTP server (`/healthz`, `/readyz`, channel routes)
- `internal/app/cli/` — tenant/user/invite/agent management commands
- `internal/app/config/` — JSON config loading with env variable resolution (`env:VAR`, `env_path:VAR`)
- `internal/domain/entity/` — 11 entity types with typed IDs
- `internal/domain/port/` — all plugin interfaces and service contracts
- `internal/domain/service/` — identity resolution, session mapping, LLM orchestration, prompt building
- `internal/infra/loader/` — `.so` plugin loading, symbol validation, panic recovery
- `pkg/plugins/` — shipped plugin source (organized as `kind/name/`)
- `pkg/logger/` — structured slog wrapper (importable by plugins)

### Multi-tenancy

Structural enforcement: all store queries require explicit `tenantID`. Single shared SQLite database with tenant ID as filter.

### Config

JSON config with sections: `runtime`, `gateway`, `agent`, `transport`, `store`, `llm` (drivers + endpoints + routing), `channels[]`, `tools[]`, `middleware[]`. See `config.example.json`. Config validation collects all errors via `errors.Join()` for a single startup error report.

## Conventions

- **Singular package names** everywhere: `entity`, `dto`, `port` (not `entities`, `dtos`, `ports`)
- Minimal dependencies: stdlib + `modernc.org/sqlite` + `google/uuid` only
- No external DI, logging, or HTTP frameworks
- Plugin config passed as `json.RawMessage` to `Init()`
- Graceful shutdown: SIGINT/SIGTERM → drain channels → drain agent loop → stop plugins in reverse order
