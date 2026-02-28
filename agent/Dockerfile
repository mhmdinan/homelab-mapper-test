FROM golang:1.26-bookworm AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o agent main.go

FROM scratch
COPY --from=builder /app/agent /agent
# Needed for communicating with Docker socket if mounted
# But scratch won't have it natively unless statically linked. The go binary handles it.

ENTRYPOINT ["/agent"]
