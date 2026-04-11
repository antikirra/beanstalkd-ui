FROM golang:1.26-alpine AS builder

RUN apk add --no-cache build-base git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /aurora ./cmd/aurora

FROM alpine:3.21
COPY --from=builder /aurora /usr/local/bin/aurora

RUN printf 'servers = ["beanstalkd:11300"]\nlisten = "0.0.0.0:3000"\nversion = 2.2\n\n[openpage]\nenabled = false\n\n[auth]\nenabled = false\npassword = "password"\nusername = "admin"\n\n[sample]\nstorage = "{}"\n' > /etc/aurora.toml

EXPOSE 3000
ENTRYPOINT ["aurora", "-c", "/etc/aurora.toml"]
