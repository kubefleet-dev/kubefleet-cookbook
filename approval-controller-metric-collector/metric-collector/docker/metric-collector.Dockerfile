FROM golang:1.24 AS builder
WORKDIR /workspace

# Copy approval-request-controller (for APIs)
COPY approval-request-controller/ approval-request-controller/

# Copy go mod files
COPY metric-collector/go.mod metric-collector/go.sum* metric-collector/
WORKDIR /workspace/metric-collector
RUN go mod download

# Copy source code
COPY metric-collector/cmd/ cmd/
COPY metric-collector/pkg/ pkg/

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -a -o metric-collector \
    ./cmd/metriccollector

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/metric-collector .
USER 65532:65532

ENTRYPOINT ["/metric-collector"]
