FROM golang:1.25-alpine AS builder
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /agentgate ./cmd/agentgw
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /agentgate-verify ./cmd/agentgate-verify
RUN mkdir /data

# modernc.org/sqlite is pure Go (github.com/Clawdlinux/agentgate/pull/20), so
# the final binary is fully static and needs no base OS image at all: no libc,
# no shell, no package manager, nothing an attacker can use once they're in
# the container. Only the CA cert bundle is copied over, for outbound TLS
# (the OAuth token refresh calls this gateway makes to upstream SaaS APIs).
FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /agentgate /usr/local/bin/agentgate
COPY --from=builder /agentgate-verify /usr/local/bin/agentgate-verify
COPY --from=builder /data /data
COPY configs/ /etc/agentgate/configs/
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/agentgate"]
CMD ["--config", "/etc/agentgate/configs/services.yaml", "--db", "/data/agentgate.db", "--addr", ":8080"]
