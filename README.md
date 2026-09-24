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
