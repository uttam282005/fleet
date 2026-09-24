# Build stage
FROM golang:alpine AS build

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build static server binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/fleet-server ./cmd/server

# Runtime stage
FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata && \
    addgroup -S appgroup && adduser -S appuser -G appgroup && \
    mkdir -p /data && chown -R appuser:appgroup /data

WORKDIR /app
COPY --from=build /bin/fleet-server /app/fleet-server

USER appuser

EXPOSE 8080

ENV FLEET_ADDR=":8080" \
    FLEET_DB_PATH="/data/fleet.db" \
    FLEET_ONLINE_WINDOW="30s"

VOLUME ["/data"]

ENTRYPOINT ["/app/fleet-server"]
