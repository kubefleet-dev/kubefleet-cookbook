package main

import (
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// Get the workload kind from environment variable
	// This should be set to the actual parent resource (e.g., "Deployment", "StatefulSet", "DaemonSet")
	// not the immediate controller like ReplicaSet
	workloadKind := os.Getenv("WORKLOAD_KIND")
	if workloadKind == "" {
		workloadKind = "Unknown"
	}

	// Define a simple gauge metric for health with workload_kind label
	workloadHealth := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "workload_health",
			Help: "Indicates if the workload is healthy (1=healthy, 0=unhealthy)",
		},
		[]string{"workload_kind"},
	)

	// Set it to 1 (healthy) with the workload kind label
	workloadHealth.WithLabelValues(workloadKind).Set(1)

	// Register metric with Prometheus default registry
	prometheus.MustRegister(workloadHealth)

	// Expose metrics endpoint
	http.Handle("/metrics", promhttp.Handler())

	// Start HTTP server
	http.ListenAndServe(":8080", nil)
}
