FROM golang:1.26-alpine AS builder

RUN apk add --no-cache build-base git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /beanstalkd-ui ./cmd/beanstalkd-ui

FROM alpine:3.21
COPY --from=builder /beanstalkd-ui /usr/local/bin/beanstalkd-ui

RUN printf 'servers = ["beanstalkd:11300"]\nlisten = "0.0.0.0:3000"\nversion = 2.2\n\n[openpage]\nenabled = false\n\n[auth]\nenabled = false\npassword = "password"\nusername = "admin"\n\n[sample]\nstorage = "{}"\n' > /etc/beanstalkd-ui.toml

EXPOSE 3000
ENTRYPOINT ["beanstalkd-ui", "-c", "/etc/beanstalkd-ui.toml"]
