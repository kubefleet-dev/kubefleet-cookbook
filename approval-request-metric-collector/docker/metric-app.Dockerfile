# Build stage
FROM golang:1.24 AS builder

WORKDIR /workspace

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY apis/ apis/
COPY pkg/ pkg/
COPY cmd/ cmd/

# Build the application
ARG GOARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH} go build \
    -a -o metric-app \
    ./cmd/metricapp

# Runtime stage
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/metric-app .
USER 65532:65532

ENTRYPOINT ["/metric-app"]
