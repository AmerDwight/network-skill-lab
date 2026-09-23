# network-skill-lab

network-skill-lab (`nsl`) is a self-hosted lab platform for practising network
troubleshooting: it provisions a small Docker topology per attempt, gives you a
terminal on every node in the browser, and checks your work against the lab's
checkpoints while it times you. The whole product ships as one Go binary with
the React frontend embedded.

## Quick start

Requires Go 1.25, Node 22 and a working Docker daemon.

```sh
make image      # build the nsl/node sandbox image
make build      # build the frontend, then ./bin/nsl
./bin/nsl serve # http://localhost:8080
```

## Development

```sh
make dev               # Vite dev server + go run ./cmd/nsl serve
make lint              # golangci-lint, eslint, prettier, tsc
make test              # go test ./... and vitest
make test-integration  # go test -tags integration ./...
```

The frontend builds into `internal/web/dist`, which `internal/web` embeds, so
any Go build or test needs `make web` (or `make build`) to have run at least
once.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `NSL_LISTEN` | `:8080` | HTTP listen address |
| `NSL_DATA_DIR` | `./data` | SQLite database and recordings |
| `NSL_CONTENT_DIR` | `./content` | Lab content root |
| `NSL_NODE_IMAGE` | `nsl/node` | Sandbox node image |
| `NSL_IDLE_TIMEOUT` | `15m` | Idle attempt timeout |
| `NSL_CHECK_INTERVAL` | `5s` | Checkpoint evaluation interval |
| `NSL_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |

## License

MIT, see [LICENSE](LICENSE).
