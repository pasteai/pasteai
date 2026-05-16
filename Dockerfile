# ── Build stage ────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

RUN adduser -D -u 10001 pasteai && mkdir /data && chown 10001:10001 /data

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /pasteai ./cmd/pasteai

# ── Runtime stage ──────────────────────────────────────────
FROM scratch

COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /pasteai /pasteai

# Data directory for the bbolt database (owned by pasteai user)
COPY --from=builder --chown=10001:10001 /data /data
VOLUME ["/data"]

EXPOSE 8080

USER 10001:10001

ENTRYPOINT ["/pasteai"]
CMD ["serve", "--addr", ":8080", "--db", "/data/documents.db"]
