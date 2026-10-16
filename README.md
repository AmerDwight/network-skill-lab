# network-skill-lab

network-skill-lab (`nsl`) is a self-hosted lab platform for practising network
troubleshooting: it provisions a small Docker topology per attempt, gives you a
terminal on every node in the browser, and checks your work against the lab's
checkpoints while it times you. The whole product ships as one Go binary with
the React frontend embedded.

## Requirements

- Docker Engine, running and reachable by the current user
- Go 1.25
- Node 22
- golangci-lint (only needed for `make lint`)

## Quick start

```sh
make image                      # build the nsl/node sandbox image
make build                      # build the frontend, then ./bin/nsl
NSL_LISTEN=:8081 ./bin/nsl serve
```

`make image` takes about 7 minutes and leaves roughly 2 GB on disk; it only has
to run again when `images/node` changes. The default listen address is `:8080`,
which is often already taken, hence `NSL_LISTEN=:8081` above.

Open <http://localhost:8081>. No login. Pick the fixture lab
`net-ip-01-link-down` and press Start; the only mode is guided. Within about
15 seconds the attempt page shows a terminal for each of the two nodes and the
ticket describing the fault. Fix it from the terminals: checkpoints tick within
5 seconds of a fix, and when all of them pass the timer stops, the sandbox is
destroyed and the result page shows the total time, the command count, the
time each checkpoint passed, and the solution.

## Development

```sh
make dev               # Vite dev server + go run ./cmd/nsl serve
make lint              # golangci-lint, eslint, prettier, tsc
make test              # go test ./... and vitest
make test-integration  # Docker integration tests, needs a Docker daemon
make test-vm           # VM provider integration tests, needs KVM, libvirt and NSL_VM_IMAGE
```

`VITE_API_TARGET` points the Vite dev server at a different backend (default
`http://127.0.0.1:8080`). `npm run mock` inside `web/` serves a fake API and
WebSocket on port 18090, so the frontend can run without Go or Docker:

```sh
cd web && npm run mock
VITE_API_TARGET=http://127.0.0.1:18090 npm run dev
```

The frontend builds into `internal/web/dist`, which `internal/web` embeds, so
any Go build or test needs `make web` (or `make build`) to have run at least
once.

## Sandbox host

Every sandbox node is a privileged container running systemd, so a few host
resources are shared by all of them.

`fs.inotify.max_user_instances` is shared by every container running as root,
and each node needs several instances for systemd, udevd and journald. The WSL2
default of 128 is exhausted at roughly a dozen concurrent nodes, after which
`systemd-udevd` fails to start and netplan cannot apply the node's addresses.
Raise it on the host before running more than a couple of sandboxes:

```sh
sudo sysctl -w fs.inotify.max_user_instances=1024
sudo sysctl -w fs.inotify.max_user_watches=524288
```

Make it permanent with a line in `/etc/sysctl.d/99-nsl.conf`. `nsl serve` logs a
warning at startup when the limit is below 512.

Memory is the other limit. A settled `ubuntu` or `ubuntu-nm` node uses about
30 MB, a `k3s-agent` about 200 MB and a `k3s-server` about 600 MB, so budget
roughly 1 GB for a two-node k3s lab and leave headroom for the image pulls
during setup. `/api/health` reports the memory still available.

Two `nsl` processes on the same Docker daemon must use different data dirs.
Garbage collection only touches resources labelled with the process's own
instance id, which is derived from the absolute data dir, or taken from
`NSL_INSTANCE`; `/api/health` reports it as `instance`. `nsl gc` removes the
leftover sandboxes of that instance by hand.

## VM runner

Labs with `environment: vm` run on a remote runner started with
`nsl runner --provider vm`. Each node is a KVM virtual machine booted from a
copy-on-write overlay of one base image; commands and checkpoints run through
the QEMU guest agent and the terminal is an SSH session on the management
network.

Host requirements:

- `/dev/kvm` (on WSL2, `nestedVirtualization=true` in `.wslconfig`), libvirt
  with QEMU, `qemu-img` and `genisoimage`.
- The runner's user in the `libvirt` group: the provider calls
  `virsh -c qemu:///system`.
- The libvirt network `default` (NAT) for management, or another one named by
  `NSL_VM_MGMT_NETWORK`.
- The image and the work dir where QEMU can read them: QEMU runs as
  `libvirt-qemu`, so neither may sit under a home directory, every directory
  above the image must be `o+x` and the work dir must be `o+rx`. The runner
  refuses to start otherwise.

Build the base image once (3 to 11 minutes, mostly package downloads):

```sh
NSL_VM_IMAGE_DIR=/var/lib/nsl/images images/vm/build.sh
sudo mkdir -p /var/lib/nsl/vm && sudo chown "$USER" /var/lib/nsl/vm && chmod o+rx /var/lib/nsl/vm
```

Then start the runner:

```sh
NSL_VM_IMAGE=/var/lib/nsl/images/nsl-node.qcow2 \
  nsl runner --provider vm --url wss://lab.example.com/ws/runner --name vm-01 --token-file vm-01.token
```

| Variable | Default | Purpose |
| --- | --- | --- |
| `NSL_VM_IMAGE` | (required) | Path to `nsl-node.qcow2` |
| `NSL_VM_DIR` | `/var/lib/nsl/vm` | Overlays, seed ISOs, SSH keys and serial logs, one directory per sandbox |
| `NSL_VM_MGMT_NETWORK` | `default` | libvirt network for the management interface `eth0` |
| `NSL_VM_MEMORY_MIB` | `1024` | Memory per VM |
| `NSL_VM_VCPUS` | `1` | vCPUs per VM |
| `NSL_VM_BOOT_TIMEOUT` | `120s` | How long provisioning waits for the guest agent |

A two-node lab starts in about 50 seconds and each VM takes about 600 MB of
host memory, so size `--max-sandboxes` accordingly. `make test-vm` runs the VM
integration tests when `NSL_VM_IMAGE` is set.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `NSL_LISTEN` | `:8080` | HTTP listen address |
| `NSL_DATA_DIR` | `./data` | SQLite database and recordings |
| `NSL_CONTENT_DIR` | `./content` | Lab content root |
| `NSL_NODE_IMAGE` | `nsl/node` | Sandbox node image |
| `NSL_INSTANCE` | hash of the data dir | Label that scopes garbage collection |
| `NSL_IDLE_TIMEOUT` | `15m` | Idle attempt timeout |
| `NSL_CHECK_INTERVAL` | `5s` | Checkpoint evaluation interval |
| `NSL_SYSTEMD_TIMEOUT` | `60s` | How long provisioning waits for systemd on a node |
| `NSL_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |

## License

MIT, see [LICENSE](LICENSE).

## Deployment

`nsl serve` speaks plain HTTP. Put it behind a reverse proxy that terminates TLS and forwards `X-Forwarded-Proto: https` (the session cookie is then marked `Secure`) and passes WebSocket upgrades for `/ws/`. Minimal nginx example:

```
location / {
    proxy_pass http://127.0.0.1:8081;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $remote_addr;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 3600s;
}
```

First run: create an admin with `nsl user add <name> --role admin`, then sign in. Set `NSL_MAX_SANDBOXES` to what the host can hold (about 30 MB per Ubuntu node, 200 MB per k3s agent, 600 MB per k3s server). One `nsl serve` per data directory; the sandbox host must stay on a trusted network until the remote runner exists (DESIGN.md R6).

Retention: `nsl prune --older-than 30d [--dry-run]` deletes finished attempts that ended before the cutoff, with their recordings and command history. Set `NSL_PRUNE_AFTER` (for example `720h` or `30d`) to have `nsl serve` do the same at startup and every 24 hours.

### Remote runners

Sandboxes can run on other hosts. Each runner host needs Docker, the
`nsl/node` image and the same `nsl` binary; it dials out to the control plane,
so it needs no open port.

1. Register the runner on the admin page (Runners, Add a runner) or on the
   control-plane host with `nsl runner token lab-01`. The token is shown once;
   save it to a file on the runner host.
2. On the runner host run
   `nsl runner --url wss://<host>/ws/runner --name lab-01 --token-file ./lab-01.token --max-sandboxes 3`
   (or pass the token in `NSL_RUNNER_TOKEN`). The admin page then lists it as
   connected with its capacity and version.

New attempts go to the connected runner that offers the lab's environment and
has the lowest share of its capacity in use; the attempt's page shows the
runner's name. When every such runner is full, starting a lab answers 429 with
the load of each runner. The in-process runner is called `local`, takes
`NSL_MAX_SANDBOXES` sandboxes, and is left out when the control-plane host has
no reachable Docker daemon.

If a runner's connection drops, its attempts end within seconds with the reason
`runner disconnected`. The runner keeps their sandboxes for two minutes; when
it reconnects the control plane destroys them, and it takes new attempts right
away. Revoking the runner on the admin page disconnects it at once and it
stops instead of reconnecting; `nsl runner revoke lab-01` only refuses its next
connection, because it runs outside the server. A runner token controls Docker
on that host, so treat it like a root credential.
