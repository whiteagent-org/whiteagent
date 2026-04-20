# Sandbox Security Model

The Docker sandbox plugin runs user code in isolated containers. For setup instructions see [deployment.md](deployment.md).

## Deployment Mode

### DinD (Docker-in-Docker) -- Default

**How it works:** A separate `docker:dind` sidecar runs its own Docker daemon. Whiteagent connects via TCP with TLS. Sandbox containers are fully nested.

**Setup:** `compose.yml`

**Security characteristics:**
- Sandbox containers are isolated from the host Docker daemon entirely
- UIDs inside the dind VM have no mapping to host UIDs
- Path traversal attacks are contained within the dind shared volume
- No host socket exposure -- even a container escape only reaches the dind daemon
- TLS is enforced on the internal TCP connection via `DOCKER_TLS_CERTDIR=/certs`

**Risks:**
- The dind sidecar runs with `privileged: true` (required for nested Docker). If an attacker escapes from a sandbox into the dind container, they have full host capabilities.

### Bare Metal (Development)

**How it works:** The binary runs directly on the host and uses the host Docker socket to launch sandbox containers as siblings.

**Risks:**
- Anyone with access to the Docker socket has root-equivalent access on the host
- Container UID maps directly to host UID (e.g., UID 1000 in sandbox = UID 1000 on host)

**Mitigation:** Enable Docker [user namespace remapping](https://docs.docker.com/engine/security/userns-remap/) at the daemon level for full UID isolation. Only recommended for development.

## Container Hardening

Every sandbox container is created with:

| Control | Setting | Purpose |
|---------|---------|---------|
| Capabilities | `CAP_DROP ALL` | No Linux capabilities granted |
| Privilege escalation | `no-new-privileges` | Prevents setuid/setgid binaries |
| Root filesystem | Read-only | Prevents persistent modifications |
| User | Configurable UID:GID (default 1000:1000) | Non-root execution |
| CPU | Configurable (default 0.5 cores) | Prevents CPU starvation |
| Memory | Configurable (default 256 MB) | Prevents OOM |
| PIDs | Configurable (default 100) | Prevents fork bombs |
| Ulimits | nofile 1024/2048, nproc matches pids_limit | File descriptor and process limits |
| Tmpfs | `/tmp`, `/var/tmp` (configurable size), `/message` (owner-only) | Writable scratch space |
| Network | Configurable (`allow_network`, default true) | `bridge` or `none` |
| Seccomp | Docker default profile | Blocks ~50 dangerous syscalls |

## Configuration Reference

Sandbox config fields with security implications (`config.json` > `sandbox` > `config`):

| Field | Default | Description |
|-------|---------|-------------|
| `allow_network` | `true` | Set to `false` to fully isolate sandbox from network |
| `container_uid` | `1000` | UID for processes inside the sandbox container |
| `container_gid` | `1000` | GID for processes inside the sandbox container |
| `resources.cpu_cores` | `0.5` | CPU limit per container |
| `resources.memory_mb` | `256` | Memory limit per container |
| `resources.pids_limit` | `100` | Max processes per container |
| `resources.tmpfs_mb` | `64` | Tmpfs size for /tmp, /var/tmp, /message |
| `exec_timeout` | `5m` | Per-command execution timeout |
| `idle_timeout` | `15m` | Container recycled after inactivity |
| `host_data_dir` | `""` | Required for bare-metal mode: host path to data directory. Empty for DinD. |
