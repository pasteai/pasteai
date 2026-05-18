# ── Build stage ────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /pasteai ./cmd/pasteai

# ── Runtime stage ──────────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache su-exec && mkdir /data

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /pasteai /pasteai
COPY docker-entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

VOLUME ["/data"]
EXPOSE 8080

ENTRYPOINT ["/entrypoint.sh"]
CMD ["/pasteai", "serve", "--addr", ":8080", "--db", "/data/documents.db"]
