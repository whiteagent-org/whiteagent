# Deployment Guide

Three deployment strategies are available, each with different tradeoffs between simplicity, isolation, and resource overhead.

All strategies use the same application image (`docker/app/Dockerfile`) and the same `config.json` format. The difference is how the sandbox plugin connects to a Docker daemon to run user code containers.

## Prerequisites

Regardless of strategy, you need:

1. A `config.json` (see `config.example.json`)
2. An `.env` file with API keys (see `.env.example`)
3. Plugin `.so` files (built via `make plugins` or included in the Docker image)

## Strategy 1: DinD (Docker-in-Docker)

**The simplest fully-containerized option.** Everything runs in Docker with no host dependencies beyond Docker itself.

A dedicated `docker:dind` sidecar runs its own Docker daemon. Whiteagent connects to it via TLS over TCP. Sandbox containers are fully nested inside the DinD daemon.

### Setup

```bash
# 1. Generate TLS certificates (one-time)
make dind-certs

# 2. Place config and env files
cp config.example.json data/config.json  # edit as needed
cp .env.example .env                      # fill in API keys

# 3. Start
docker compose -f compose.dind.yml up -d
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

## Strategy 3: DooD (Docker-outside-of-Docker)

**Whiteagent runs in a container** but shares the host Docker daemon via a socket mount. Sandbox containers run as sibling containers on the host.

### Setup

```bash
# 1. Place config and env files
cp config.example.json config.json  # edit as needed
cp .env.example .env                # fill in API keys

# 2. Start
docker compose -f compose.dood.yml up -d
```

### Config adjustments

`host_data_dir` must match the host-side path to the data directory, because bind mounts are resolved by the host daemon:

```json
"sandbox": {
  "config": {
    "socket_path": "/var/run/docker.sock",
    "host_data_dir": "/home/deploy/whiteagent/data",
    "services": "./compose.services.yml"
  }
}
```

If you use `compose.dood.yml` with `DATA_DIR=./data`, then `host_data_dir` should be the absolute path to `./data` on the host.

### Characteristics

- Lower overhead than DinD (no nested daemon)
- Sandbox containers share the host Docker daemon
- UID mapping: container UIDs map directly to host UIDs
- Docker socket mounted into whiteagent container (never into sandbox containers)

**Mitigation:** Enable Docker [user namespace remapping](https://docs.docker.com/engine/security/userns-remap/) at the daemon level for full UID isolation.

## Sidecar Services

All three strategies use `compose.services.yml` to define sidecar services (e.g., browserless for headless Chrome). The sandbox plugin reads this file, pulls images, and starts containers on the sandbox network.

To add a new service, append it to `compose.services.yml` following the existing format. Services must include a `healthcheck` so whiteagent can wait for readiness.

## Comparison

| | DinD | Bare Metal + Services | DooD |
|---|---|---|---|
| Complexity | Low | Medium | Low |
| Host Docker socket exposed | No | Yes | Yes (in container) |
| UID isolation | Full | None (host UIDs) | None (host UIDs) |
| Container escape blast radius | DinD daemon | Host | Host |
| Resource overhead | Higher (nested daemon) | Lowest | Low |
| Best for | Production | Development | Staging / simple deploys |

For detailed security analysis, see [security-model.md](security-model.md).
