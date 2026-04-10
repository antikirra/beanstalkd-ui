# beanstalkd-ui

Web console for [Beanstalkd](https://beanstalkd.github.io/). Single Go binary with embedded assets.

Maintained fork of [xuri/aurora](https://github.com/xuri/aurora) — modernized, secured, and cleaned up. Designed to work alongside [antikirra/beanstalkd](https://github.com/antikirra/beanstalkd).

## Quick start

```sh
go build -o beanstalkd-ui . && ./beanstalkd-ui
docker build -t beanstalkd-ui . && docker run -p 3000:3000 beanstalkd-ui
```

Opens `http://127.0.0.1:3000`. Add beanstalkd servers through the UI or in `aurora.toml`.

## What it does

Manage multiple beanstalkd servers from a single dashboard. View tubes, inspect jobs in any state, search job content, move jobs between tubes, kick or delete in bulk. Pause and unpause tubes. Save job templates for repeated use. Collect and graph tube statistics over time.

## Configuration

```toml
servers = ["127.0.0.1:11300"]
listen = "127.0.0.1:3000"

[auth]
enabled = true
username = "admin"
password = "changeme"
```

Servers, column filters, refresh intervals, and display preferences are managed through the UI.

## License

MIT. Based on [aurora](https://github.com/xuri/aurora) by Ri Xu and contributors.
