# Multi-stage build for minimal image size
FROM golang:1.25.8-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
ENV GOTOOLCHAIN=local
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /devcontract .

# Final stage — minimal scratch image
FROM alpine:3.19

RUN apk add --no-cache ca-certificates

COPY --from=builder /devcontract /usr/local/bin/devcontract

# Create non-root user
RUN adduser -D -h /home/devcontract devcontract
USER devcontract
WORKDIR /home/devcontract

ENTRYPOINT ["devcontract"]
