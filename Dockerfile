# Multi-stage build for minimal image size
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /envsync .

# Final stage — minimal scratch image
FROM alpine:3.19

RUN apk add --no-cache ca-certificates

COPY --from=builder /envsync /usr/local/bin/envsync

# Create non-root user
RUN adduser -D -h /home/envsync envsync
USER envsync
WORKDIR /home/envsync

ENTRYPOINT ["envsync"]
