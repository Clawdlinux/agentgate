FROM golang:1.25-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /agentgate ./cmd/agentgw
RUN CGO_ENABLED=1 go build -o /agentgate-verify ./cmd/agentgate-verify

FROM alpine:3.21
RUN apk add --no-cache sqlite-libs ca-certificates
COPY --from=builder /agentgate /usr/local/bin/agentgate
COPY --from=builder /agentgate-verify /usr/local/bin/agentgate-verify
COPY configs/ /etc/agentgate/configs/
RUN mkdir -p /data
EXPOSE 8080
ENTRYPOINT ["agentgate"]
CMD ["--config", "/etc/agentgate/configs/services.yaml", "--db", "/data/agentgate.db", "--addr", ":8080"]
