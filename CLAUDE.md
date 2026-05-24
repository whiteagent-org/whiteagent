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

<!-- GSD:project-start source:PROJECT.md -->
## Project

**whiteagent**

whiteagent is a plugin-driven multi-tenant AI agent runtime in Go. A single binary loads `.so` plugins at startup, wires them through dependency injection, and runs a ReAct agent loop per inbound message — currently shipping integrations for Telegram, Teams, OpenAI-compatible LLMs, SQLite persistence, Docker sandbox, and a built-in tool/middleware library.

This milestone is a structural redesign: replace the seven fixed plugin kinds with a single capability-based plugin model, add CLI and chat command capabilities, and introduce a built-in runtime safety/confirmation layer for tool execution.

**Core Value:** Plugins must be extensible by composition, not by enumeration — a single plugin should be able to register everything it provides (tools, routes, commands, channels, middleware) through compile-time-checked capability interfaces.

### Constraints

- **Tech stack**: Go 1.24, stdlib-first (no web/DI/ORM/logging frameworks) — Maintain the project's minimal-dependency posture.
- **Plugin ABI**: `.so` plugins must still be loadable via Go's `plugin` package — That's the deployment model and is not changing.
- **Compile-time safety**: capability detection must use Go interfaces, not runtime registry tables — The whole motivation for capability interfaces is that the compiler enforces it.
- **Multi-tenancy**: any new persisted state (permissions, etc.) must take explicit `TenantID` — Structural multi-tenancy is non-negotiable in this codebase.
- **No backwards compat**: existing plugin code and config format can change freely — Pre-1.0, clean cut preferred over transition complexity.
<!-- GSD:project-end -->

<!-- GSD:stack-start source:codebase/STACK.md -->
## Technology Stack

## Languages
- Go 1.24 — entire runtime, all plugins, CLI, gateway
- Bash — build scripts (`scripts/build-plugins.sh`), Makefile targets
- Python3 — config JSON validation only (`make validate-config`)
## Runtime
- Go 1.24 (module `github.com/whiteagent-org/whiteagent`)
- Single binary entry point: `cmd/whiteagent/`
- Plugins loaded as Go `.so` files at startup via `internal/infra/loader/`
- Go modules (`go mod`)
- Lockfile: `go.sum` present and committed
## Frameworks
- No web framework — uses `net/http` stdlib `ServeMux` directly (`internal/app/gateway/gateway.go`)
- No DI framework — manual wiring in `internal/app/agent/runtime.go`
- No ORM — raw `database/sql` with `modernc.org/sqlite` driver (`pkg/plugins/store/sqlite/plugin.go`)
- Go stdlib `testing` package — no external test framework
- Standard `go test ./...` runner
- `make build` — compiles binary to `bin/whiteagent`
- `make plugins` — builds all `.so` files to `bin/plugins/` via `scripts/build-plugins.sh`
- `CGO_ENABLED=1` required for plugin `.so` builds; binary itself is CGO-free (modernc.org/sqlite is pure Go)
- `go build -buildmode=plugin` — plugin compilation mode
- Docker multi-stage build: `docker/app/Dockerfile` (builder: `golang:1.24-bookworm`, runtime: `debian:bookworm-slim`)
## Key Dependencies
- `modernc.org/sqlite v1.36.0` — pure-Go SQLite driver; no CGO required for the runtime binary; used by `pkg/plugins/store/sqlite/`
- `github.com/google/uuid v1.6.0` — UUID generation for entity IDs throughout the codebase
- `gopkg.in/yaml.v3 v3.0.1` — YAML frontmatter parsing in `pkg/yaml/`
- `modernc.org/libc v1.61.13`
- `modernc.org/mathutil v1.7.1`
- `modernc.org/memory v1.8.2`
- `github.com/dustin/go-humanize v1.0.1`
- `github.com/ncruces/go-strftime v0.1.9`
- `github.com/remyoudompheng/bigfft`
- `golang.org/x/sys v0.30.0`
- `golang.org/x/exp v0.0.0-20230315142452`
- `github.com/mattn/go-isatty v0.0.20`
- OpenAI API — `pkg/plugins/llm/openai_compat/plugin.go` (raw HTTP, SSE streaming)
- Whisper STT API — `pkg/plugins/middleware/whisper/plugin.go` (raw HTTP multipart)
- Telegram Bot API — `pkg/plugins/channel/telegram/` (raw HTTP long polling)
- Microsoft Bot Framework — `pkg/plugins/channel/teams/` (raw HTTP + JWT/JWKS + OAuth2)
- Brave Search API — `pkg/plugins/tool/web_search/plugin.go` (raw HTTP)
- Docker Engine API — `pkg/plugins/sandbox/docker/` (raw HTTP over Unix socket)
## Configuration
- JSON config file (`config.example.json`) loaded at startup via `internal/app/config/config.go`
- Env var resolution: `env:VAR_NAME` substitution in any string field
- File-based secrets: `env_path:VAR_NAME` reads path from env var, then reads file contents
- Encryption key for secrets: `runtime.encryption_key` — 32-byte (64 hex chars) AES key
- `runtime` — logging level, shutdown timeout, scheduler poll interval, timezone, encryption key, data dir, skills dir
- `gateway` — listen address (default `:8080`), public URL for webhooks
- `agent` — max iterations (25), turn timeout (60s), max workers (10), token budget (32000)
- `transport` — singleton plugin (memory-transport)
- `store` — singleton plugin (sqlite-store) with DB path
- `sandbox` — singleton plugin (docker-sandbox) with Docker socket, image, resource limits
- `llm` — drivers array (multi-endpoint), routing (primary + fallbacks), compaction settings
- `channels[]` — array of channel plugins (Telegram, Teams)
- `tools[]` — array of tool plugins
- `middleware[]` — array of middleware plugins
- `Makefile` — primary build orchestrator
- `scripts/build-plugins.sh` — auto-generates `_build/main.go` wrappers, compiles each plugin `.so` separately
- `.env` file used by Docker Compose for secret injection
## Platform Requirements
- Go 1.24+
- C toolchain (gcc/clang) required for `CGO_ENABLED=1` plugin builds
- Docker daemon (optional, for sandbox plugin)
- `python3` for `make validate-config`
- Debian/Linux (Docker image uses `debian:bookworm-slim`)
- Docker daemon or Docker-in-Docker for sandbox feature
- TLS certificates for DinD mode (`make dind-certs` generates via openssl)
- Environment variables for all secrets (never in config file values directly)
- Docker Compose (`compose.yml`) — primary deployment method
- Docker-in-Docker sidecar (`docker:dind` image) for sandbox isolation
- Port 8080 exposed (configurable via `PORT` env var)
- Volumes: config JSON (read-only), data dir, DB dir, DinD TLS certs
<!-- GSD:stack-end -->

<!-- GSD:conventions-start source:CONVENTIONS.md -->
## Conventions

## Naming Patterns
- Snake_case for multi-word filenames: `plugin_test.go`, `cron_create/plugin.go`, `agent_instructions.tmpl`
- One file per concept within a package; related helpers co-located in same package file
- Test files co-located: `message.go` → `message_test.go`
- `nyquist_test.go` suffix indicates gap-filling tests from Nyquist validation audits
- Exported: PascalCase — `NewCompletionService`, `NewResolver`, `NewPromptBuilder`
- Unexported: camelCase — `buildContextBlock`, `normalizeEnvKey`, `newTestStore`
- Constructor naming: `NewXxx` for exported, `newXxx` for package-internal
- Test helpers: `testXxx` prefix (e.g. `testMsg`, `testLoop`)
- camelCase throughout: `tenantID`, `agentID`, `userHome`, `appendErrorLogFn`
- Abbreviations kept short: `tid`, `uid`, `aid` as common test locals; `pb` for prompt builder
- Boolean field names: `indicateCalled`, `stopCalled` (plain descriptive, no `is` prefix)
- PascalCase for exported structs, interfaces, type aliases
- Typed ID types in `internal/domain/entity/ids.go` — defined types (not aliases) over bare `string`: `TenantID`, `AgentID`, `UserID`, `ConversationID`, `MessageID`, `ChatID`, `SecretID`, `CronEntryID`, `CronRunID`, `ErrorLogEntryID`
- Each typed ID implements `String() string` and `IsEmpty() bool`
- Interface names end in the behaviour they represent: `CompletionService`, `SkillLister`, `PathProvider`, `MessageHandler`
- Unexported implementations of exported interfaces: exported `CompletionService` interface, unexported `completionService` struct
- **Singular** naming enforced everywhere: `entity`, `dto`, `port`, `config`, `loader`, `agent`, `skill`, `prompt`, `loop`, `identity`
- Never `entities`, `dtos`, `ports`, `configs`, `loaders`
- Package name equals directory leaf: `pkg/logger/` → `package logger`, `internal/domain/entity/` → `package entity`
- Typed const blocks with `const ( ... )` grouping: `MessageKindMessage`, `MessageKindReaction`, `RoleSystem`, `RoleUser`, `RoleAssistant`, `RoleTool`
## Code Style
- `gofmt` enforced via `make fmt` (`gofmt -w $(find . -name '*.go' | sort)`)
- No custom formatter config — pure `gofmt` defaults
- No `.editorconfig`; tabs for indentation (Go standard)
- No `.golangci.yml` detected; linting is implicit via `gofmt` + compiler
- Build tags not used for test separation
## Import Organization
- None used. All imports use full module paths.
## Error Handling
## Logging
## Comments
## Function Design
- Interfaces over concrete types: `func NewLoop(cfg LoopConfig, completion llm.CompletionService, store port.StorePlugin, ...)`
- `context.Context` always first parameter for I/O-bound functions
- `json.RawMessage` for plugin config (passed through to `Init`)
- Go convention: `(result, error)` pair
- Named return values not used
- Functions that collect validation errors return `error` (containing joined errors)
## Module Design
- Export only what callers need. Implementation structs are unexported, interfaces exported.
- Constructor functions (`NewXxx`) exported; underlying struct unexported.
- Not used. Each package exposes symbols directly; no `package.go` re-export files.
- `//go:embed` used for templates and static files: `prompt.tmpl`, `compaction.tmpl`, `agent_instructions.tmpl`, HTML templates in `gateway/templates/`
- `skills/skills.go` embeds the entire skills directory via `//go:embed all:*`
<!-- GSD:conventions-end -->

<!-- GSD:architecture-start source:ARCHITECTURE.md -->
## Architecture

## High-Level Pattern
- **`internal/domain/`** — pure domain. Entities, ports (plugin interfaces + service contracts), and domain services. No I/O implementations, no infra.
- **`internal/app/`** — application orchestration. Runtime wiring, agent loop, HTTP gateway, CLI commands, config loading.
- **`internal/infra/`** — infrastructure. `.so` plugin loader, scoped filesystem, built-in tool/middleware implementations.
## Seven Plugin Kinds
```go
```
| Kind | Port file | Role | Singleton? |
|------|-----------|------|------------|
| Store | `internal/domain/port/plugin_store.go` | Persistence (SQLite) | Yes |
| Transport | `internal/domain/port/plugin_transport.go` | Topic-based pub/sub | Yes |
| Channel | `internal/domain/port/plugin_channel.go` | External platform adapter | No |
| LLM | `internal/domain/port/plugin_llm.go` | Model API completions | No |
| Tool | `internal/domain/port/plugin_tool.go` | LLM tool execution | No |
| Middleware | `internal/domain/port/plugin_middleware.go` | Handler pipeline wrapping | No |
| Sandbox | `internal/domain/port/plugin_sandbox.go` | Code execution isolation | Yes |
## Entry Points
- **`cmd/whiteagent/main.go`** — CLI dispatcher. Subcommands: `serve`, `tenant`, `user`, `invite`, `agent`, `tenant-mapping`. `serve` boots the runtime; the rest are admin commands in `internal/app/cli/`.
- **`internal/app/agent/runtime.go:NewRuntime`** — constructs the `Runtime` struct.
- **`Runtime.Start(ctx)`** at `internal/app/agent/runtime.go:88` — loads plugins, builds services, injects deps, starts the lifecycle.
- **`internal/app/gateway/`** — HTTP server: `/healthz`, `/readyz`, channel webhook routes, secret submission form.
## Message Flow
```
```
```go
```
## Core Abstractions
- `loop/` — ReAct agent loop (`Loop`, `LoopConfig`, `ParseLoopConfig`)
- `prompt/` — prompt assembly with embedded templates (`prompt.tmpl`, `compaction.tmpl`)
- `llm/` — `CompletionService` with model router and provider cool-down
- `identity/` — resolves incoming raw IDs into `entity.Tenant/User/Agent/Conversation/Chat`
- `onboarding/` — invite-code redemption flow
- `conversation/` — conversation lifecycle (create, reset)
- `compaction/` — context-window management via summaries
- `mapper/` — session/chat mapping
- `scheduler/` — cron scheduling (uses `pkg/cron/`)
- `secret/` — secrets storage, redaction, per-user web form tokens
- `skill/` — skill discovery and state management (`/skills` directory + embedded global skills)
- `outbound/` — outbound message dispatch helpers
## Plugin Loading
- `Manifest` — a `func() port.PluginManifest` consulted **before** instantiation for early kind validation.
- `NewPlugin` — a `func() port.Plugin` factory.
## Multi-Tenancy
```go
```
## Configuration
## Concurrency Model
- **Per-session ordering:** `Loop.Handle` holds a `*sync.Mutex` keyed by `ChatID` from `Loop.sessions sync.Map`, so messages within a chat are processed serially.
- **Bounded concurrency:** `Loop.semaphore chan struct{}` with `cap = MaxWorkers` bounds total concurrent ReAct turns.
- **Per-turn timeout:** `context.WithTimeout(ctx, cfg.TurnTimeout)` wraps each turn.
- **Graceful drain:** `Loop.draining atomic.Bool` rejects new messages during shutdown; main shutdown path in `cmd/whiteagent/main.go` honors `runtime.shutdown_timeout` (default 30s).
- **Goroutines:** spawned mainly inside channels (poller/webhook handlers) and `Loop.Handle` subscribers. No global goroutine pool; structured shutdown via `Stop(ctx)`.
## Embedded Assets
- `internal/domain/entity/agent_instructions.tmpl` — `//go:embed`
- `internal/domain/service/prompt/prompt.tmpl` — `//go:embed`
- `internal/domain/service/compaction/compaction.tmpl` — `//go:embed`
- `internal/app/gateway/templates/*.html` — `//go:embed`
- `skills/skills.go` embeds the entire `skills/` directory tree via `//go:embed all:*`; `skills.Extract("./skills/")` writes them to disk at startup (binary is source of truth).
## Notable Design Choices
- **No DI framework.** Dependencies wired by hand in `Runtime.Start`. Reads top-to-bottom like a script.
- **No HTTP framework.** `net/http` stdlib only in `internal/app/gateway/`.
- **No logging framework.** `log/slog` stdlib wrapped by `pkg/logger/` with `WithTenantID`/`WithComponent` context enrichers.
- **No ORM.** SQL is hand-written in plugins; `pkg/plugins/store/sqlite/` uses `modernc.org/sqlite` (pure-Go, no CGO).
- **CGO required only for plugins.** `make plugins` sets `CGO_ENABLED=1` because Go's `plugin` package depends on it; the main binary builds without CGO.
<!-- GSD:architecture-end -->

<!-- GSD:skills-start source:skills/ -->
## Project Skills

| Skill | Description | Path |
|-------|-------------|------|
| openspec-apply-change | Implement tasks from an OpenSpec change. Use when the user wants to start implementing, continue implementation, or work through tasks. | `.claude/skills/openspec-apply-change/SKILL.md` |
| openspec-archive-change | Archive a completed change in the experimental workflow. Use when the user wants to finalize and archive a change after implementation is complete. | `.claude/skills/openspec-archive-change/SKILL.md` |
| openspec-explore | Enter explore mode - a thinking partner for exploring ideas, investigating problems, and clarifying requirements. Use when the user wants to think through something before or during a change. | `.claude/skills/openspec-explore/SKILL.md` |
| openspec-propose | Propose a new change with all artifacts generated in one step. Use when the user wants to quickly describe what they want to build and get a complete proposal with design, specs, and tasks ready for implementation. | `.claude/skills/openspec-propose/SKILL.md` |
<!-- GSD:skills-end -->

<!-- GSD:workflow-start source:GSD defaults -->
## GSD Workflow Enforcement

Before using Edit, Write, or other file-changing tools, start work through a GSD command so planning artifacts and execution context stay in sync.

Use these entry points:
- `/gsd-quick` for small fixes, doc updates, and ad-hoc tasks
- `/gsd-debug` for investigation and bug fixing
- `/gsd-execute-phase` for planned phase work

Do not make direct repo edits outside a GSD workflow unless the user explicitly asks to bypass it.
<!-- GSD:workflow-end -->

<!-- GSD:profile-start -->
## Developer Profile

> Profile not yet configured. Run `/gsd-profile-user` to generate your developer profile.
> This section is managed by `generate-claude-profile` -- do not edit manually.
<!-- GSD:profile-end -->
