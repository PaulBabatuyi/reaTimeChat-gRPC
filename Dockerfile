### multi-stage Dockerfile for reaTimeChat-gRPC cmd/api
FROM golang:1.25-bullseye AS builder
WORKDIR /src

# Copy modules manifests first (caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy everything and build the binary
COPY . .
WORKDIR /src/cmd/api
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags='-s -w' -o /out/reaTimeChat-api .

### final image
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /out/reaTimeChat-api /usr/local/bin/reaTimeChat-api
EXPOSE 50051
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/reaTimeChat-api"]
