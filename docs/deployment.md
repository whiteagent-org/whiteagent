# Deployment Guide

Two deployment strategies are available:

- **DinD (Docker-in-Docker)** — default, fully containerized with strong sandbox isolation.
- **Bare Metal + Docker Services** — the binary runs on the host; only sidecar services run in Docker.

Both strategies use the same application image (`docker/app/Dockerfile`) and the same `config.json` format. The difference is how the sandbox plugin connects to a Docker daemon to run user code containers.

## Prerequisites

Regardless of strategy, you need:

1. A `config.json` (see `config.example.json`)
2. An `.env` file with API keys (see `.env.example`)
3. Plugin `.so` files (built via `make plugins` or included in the Docker image)

## Strategy 1: DinD (Docker-in-Docker) — Default

**The default fully-containerized option.** Everything runs in Docker with no host dependencies beyond Docker itself.

A dedicated `docker:dind` sidecar runs its own Docker daemon. Whiteagent connects to it via TLS over TCP. Sandbox containers are fully nested inside the DinD daemon.

### Setup

```bash
# 1. Generate TLS certificates (one-time)
make dind-certs

# 2. Place config and env files
cp config.example.json data/config.json  # edit as needed
cp .env.example .env                      # fill in API keys

# 3. Start
docker compose up -d
```

### Config adjustments

In `config.json`, set the sandbox socket to the DinD daemon:

```json
"sandbox": {
  "config": {
    "socket_path": "tcp://dind:2376",
    "host_data_dir": "",
    "services": "./compose.services.yml"
  }
}
```

`host_data_dir` must be empty -- DinD shares data via a common volume mount, so no host-side path translation is needed.

### Characteristics

- Full UID isolation (DinD UIDs have no host mapping)
- No host Docker socket exposure
- Container escapes are contained within the DinD daemon
- DinD sidecar requires `privileged: true`
- Higher resource overhead (nested Docker daemon)

## Strategy 2: Bare Metal + Docker Services

**Run the binary directly on the host**, with only sidecar services (browserless, etc.) in Docker. Best for development or when you want direct control over the process.

### Setup

```bash
# 1. Build the binary and plugins
make build

# 2. Start sidecar services
docker compose -f compose.services.yml up -d

# 3. Configure and run
cp config.example.json config.json  # edit as needed
cp .env.example .env
source .env
bin/whiteagent serve --config config.json
```

### Config adjustments

The binary uses the host Docker socket directly. Set `host_data_dir` to the absolute host path of your data directory so Docker can resolve bind mounts:

```json
"sandbox": {
  "config": {
    "socket_path": "/var/run/docker.sock",
    "host_data_dir": "/absolute/path/to/data",
    "network": "sandbox",
    "services": "./compose.services.yml"
  }
}
```

### Network note

When running bare-metal, the sandbox plugin creates its own Docker network (named by `sandbox.config.network`). Sidecar services from `compose.services.yml` are attached to this network, so sandbox containers can reach them by hostname.

If you run `docker compose -f compose.services.yml up -d` separately, whiteagent will still manage the service containers at startup -- the compose file is just a declarative spec that the sandbox plugin reads and reconciles.

### Characteristics

- Simplest for local development (`make run`)
- Direct access to logs and process control
- Host Docker socket is used -- sandbox containers are siblings on the host daemon
- Requires `host_data_dir` to be set correctly

## Sidecar Services

Both strategies use `compose.services.yml` to define sidecar services (e.g., browserless for headless Chrome). The sandbox plugin reads this file, pulls images, and starts containers on the sandbox network.

To add a new service, append it to `compose.services.yml` following the existing format. Services must include a `healthcheck` so whiteagent can wait for readiness.

## Comparison

| | DinD (default) | Bare Metal + Services |
|---|---|---|
| Complexity | Low | Medium |
| Host Docker socket exposed | No | Yes |
| UID isolation | Full | None (host UIDs) |
| Container escape blast radius | DinD daemon | Host |
| Resource overhead | Higher (nested daemon) | Lowest |
| Best for | Production / general use | Development |

For detailed security analysis, see [security-model.md](security-model.md).
