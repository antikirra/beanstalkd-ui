FROM golang:1.26-alpine AS builder

RUN apk add --no-cache build-base git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /beanstalkd-ui ./cmd/beanstalkd-ui

FROM alpine:3.21
COPY --from=builder /beanstalkd-ui /usr/local/bin/beanstalkd-ui

VOLUME /data
EXPOSE 3000
ENTRYPOINT ["beanstalkd-ui", "-l", "0.0.0.0:3000", "-d", "/data/beanstalkd-ui.db"]
