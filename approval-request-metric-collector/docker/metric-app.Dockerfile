# Build stage
FROM golang:1.24-alpine AS builder
WORKDIR /workspace

# Initialize go module for metric-app
RUN go mod init metric-app && \
    go get github.com/prometheus/client_golang/prometheus@latest && \
    go get github.com/prometheus/client_golang/prometheus/promhttp@latest

# Copy source code
COPY cmd/metricapp/ ./

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o metric-app main.go

# Run stage
FROM alpine:3.18
WORKDIR /app
COPY --from=builder /workspace/metric-app .
EXPOSE 8080
CMD ["./metric-app"]
