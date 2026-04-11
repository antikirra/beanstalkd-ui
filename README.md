# Beanstalkd UI

Modern web console for [Beanstalkd](https://beanstalkd.github.io/) queue server. Single Go binary with embedded assets — no external dependencies, no npm, no build step.

Designed to work alongside [antikirra/beanstalkd](https://github.com/antikirra/beanstalkd).

## Features

- Real-time dashboard with configurable auto-refresh (idiomorph DOM diffing)
- Multiple beanstalkd servers in one interface
- Tube management: inspect, pause, kick, delete, move jobs between tubes
- Job inspection: peek ready/delayed/buried, view formatted JSON, search by content
- Sample jobs: save job templates, load into any tube
- Statistics collection with time-series data
- Connection pooling with read/write separation
- Strict CSP headers, Basic Auth with throttle, recovery middleware
- HTMX + vanilla JS — no jQuery, no Bootstrap, no frameworks

## Quick start

```sh
go build -o beanstalkd-ui ./cmd/beanstalkd-ui
./beanstalkd-ui
```

Opens at `http://127.0.0.1:3000`. Add beanstalkd servers through the UI.

## Docker

```sh
docker compose up --build
```

UI: http://localhost:3000, beanstalkd: localhost:11300.

## Configuration

All runtime settings are passed via CLI flags and environment variables — no config files.

**CLI flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `-l` | `127.0.0.1:3000` | HTTP listen address |
| `-d` | `beanstalkd-ui.db` | Path to database file |
| `-v` | | Show version and exit |

**Environment variables:**

| Variable | Description |
|----------|-------------|
| `BEANSTALKD_UI_PASSWORD` | If set, enables Basic Auth with username `beanstalkd` |

**Persistent storage:** bbolt database (`beanstalkd-ui.db`) stores server list and sample jobs. Created automatically on first run.

**Display preferences** (refresh interval, column filters, JSON formatting) are configured in Settings and stored in browser cookies.

## Architecture

```
cmd/beanstalkd-ui/   entry point, CLI flags, graceful shutdown
internal/
  api/               HTTP handlers, middleware, templates, static assets
  pool/              beanstalkd connection pool (read/write separation)
  store/             bbolt persistent storage
  model/             data types and constants
```

## License

MIT
