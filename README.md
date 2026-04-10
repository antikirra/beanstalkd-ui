# Beanstalkd UI

Modern web console for [Beanstalkd](https://beanstalkd.github.io/) queue server. Single Go binary with embedded assets — no external dependencies, no npm, no build step.

Maintained fork of [xuri/aurora](https://github.com/xuri/aurora). Designed to work alongside [antikirra/beanstalkd](https://github.com/antikirra/beanstalkd).

## Features

- Real-time dashboard with configurable auto-refresh (idiomorph DOM diffing)
- Multiple beanstalkd servers in one interface
- Tube management: inspect, pause, kick, delete, move jobs between tubes
- Job inspection: peek ready/delayed/buried, view formatted JSON, search by content
- Sample jobs: save job templates, load into any tube
- Statistics collection with time-series data
- Strict CSP headers, Basic Auth with throttle, recovery middleware
- HTMX + vanilla JS — no jQuery, no Bootstrap, no frameworks

## Quick start

```sh
go build -o beanstalkd-ui ./cmd/aurora
./beanstalkd-ui
```

Opens at `http://127.0.0.1:3000`. Add beanstalkd servers through the UI or in `aurora.toml`.

## Docker

```sh
docker build -t beanstalkd-ui .
docker run -p 3000:3000 beanstalkd-ui
```

## Configuration

`aurora.toml` is auto-created on first run:

```toml
servers = ["127.0.0.1:11300"]
listen = "127.0.0.1:3000"

[auth]
enabled = false
username = "admin"
password = "password"
```

Display preferences (refresh interval, column filters, JSON formatting) are configured in Settings.

## Architecture

```
cmd/aurora/          entry point, graceful shutdown
internal/api/        HTTP handlers, middleware, templates, static assets
internal/config/     TOML configuration
internal/model/      data types and constants
beanstalk/           forked beanstalkd client library
```

## License

MIT. Based on [aurora](https://github.com/xuri/aurora) by Ri Xu and contributors.
