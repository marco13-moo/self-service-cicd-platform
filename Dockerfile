# ---- build stage ----
FROM golang:1.25-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

# Copy module files first
COPY control-plane/go.mod control-plane/go.sum ./control-plane/
WORKDIR /app/control-plane
RUN go mod download

# Copy the rest of the control-plane source
COPY control-plane ./

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o /control-plane ./cmd/control-plane

# ---- runtime stage ----
FROM gcr.io/distroless/base-debian12

WORKDIR /app

COPY --from=builder /control-plane /app/control-plane

EXPOSE 8080

ENTRYPOINT ["/app/control-plane"]
