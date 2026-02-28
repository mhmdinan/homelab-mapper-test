FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO_ENABLED=0 is allowed since modernc.org/sqlite is purely Go.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o server main.go

FROM scratch
COPY --from=builder /app/server /server
COPY --from=builder /app/public /public
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

ENV PORT=8080
ENV DB_PATH=/data/data.db

EXPOSE 8080
VOLUME ["/data"]

ENTRYPOINT ["/server"]
