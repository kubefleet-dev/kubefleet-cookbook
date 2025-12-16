/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	autoapprovev1alpha1 "github.com/kubefleet-dev/kubefleet-cookbook/approval-request-metric-collector/apis/autoapprove/v1alpha1"
	metriccollector "github.com/kubefleet-dev/kubefleet-cookbook/approval-request-metric-collector/pkg/controllers/metriccollector"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

var (
	hubQPS            = flag.Int("hub-qps", 100, "QPS for hub cluster client")
	hubBurst          = flag.Int("hub-burst", 200, "Burst for hub cluster client")
	metricsAddr       = flag.String("metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	probeAddr         = flag.String("health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	leaderElectionID  = flag.String("leader-election-id", "metric-collector-leader", "The leader election ID.")
	enableLeaderElect = flag.Bool("leader-elect", true, "Enable leader election for controller manager.")
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	klog.InfoS("Starting MetricCollector Controller")

	// Get member cluster identity
	memberClusterName := os.Getenv("MEMBER_CLUSTER_NAME")
	if memberClusterName == "" {
		klog.ErrorS(nil, "MEMBER_CLUSTER_NAME environment variable not set")
		os.Exit(1)
	}

	// Construct hub namespace
	hubNamespace := fmt.Sprintf("fleet-member-%s", memberClusterName)
	klog.InfoS("Using hub namespace", "namespace", hubNamespace, "memberCluster", memberClusterName)

	// Build hub cluster config
	hubConfig, err := buildHubConfig()
	if err != nil {
		klog.ErrorS(err, "Failed to build hub cluster config")
		os.Exit(1)
	}
	hubConfig.QPS = float32(*hubQPS)
	hubConfig.Burst = *hubBurst

	// Start controller
	if err := Start(ctrl.SetupSignalHandler(), hubConfig, memberClusterName, hubNamespace); err != nil {
		klog.ErrorS(err, "Failed to start controller")
		os.Exit(1)
	}
}

// buildHubConfig creates hub cluster config using token-based authentication
// with TLS verification disabled (insecure mode)
func buildHubConfig() (*rest.Config, error) {
	hubURL := os.Getenv("HUB_SERVER_URL")
	if hubURL == "" {
		return nil, fmt.Errorf("HUB_SERVER_URL environment variable not set")
	}

	// Get token path (defaults to /var/run/secrets/hub/token)
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "/var/run/secrets/hub/token"
	}

	// Read token file
	tokenData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read hub token from %s: %w", configPath, err)
	}

	klog.InfoS("Using token-based authentication with insecure TLS for hub cluster")

	// Create hub config with token auth and insecure TLS
	return &rest.Config{
		Host:        hubURL,
		BearerToken: string(tokenData),
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true,
		},
	}, nil
}

// Start starts the controller with hub cluster connection
func Start(ctx context.Context, hubCfg *rest.Config, memberClusterName, hubNamespace string) error {
	// Create scheme with required APIs
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add client-go scheme: %w", err)
	}
	if err := autoapprovev1alpha1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add autoapprove v1alpha1 API to scheme: %w", err)
	}
	if err := placementv1beta1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add placement v1beta1 API to scheme: %w", err)
	}

	// Create hub cluster manager - watches MetricCollectorReport in hub namespace
	hubMgr, err := ctrl.NewManager(hubCfg, ctrl.Options{
		Scheme: scheme,
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				hubNamespace: {}, // Only watch fleet-member-<memberClusterName>
			},
		},
		Metrics: metricsserver.Options{
			BindAddress: *metricsAddr,
		},
		HealthProbeBindAddress: *probeAddr,
		LeaderElection:         *enableLeaderElect,
		LeaderElectionID:       *leaderElectionID,
	})
	if err != nil {
		return fmt.Errorf("failed to create hub manager: %w", err)
	}

	// Setup MetricCollectorReport controller (watches hub, queries member Prometheus)
	if err := (&metriccollector.Reconciler{
		HubClient: hubMgr.GetClient(),
	}).SetupWithManager(hubMgr); err != nil {
		return fmt.Errorf("failed to setup controller: %w", err)
	}

	// Add health checks
	if err := hubMgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("failed to add healthz check: %w", err)
	}
	if err := hubMgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("failed to add readyz check: %w", err)
	}

	klog.InfoS("Starting MetricCollector controller",
		"hubUrl", hubCfg.Host,
		"hubNamespace", hubNamespace,
		"memberCluster", memberClusterName,
		"metricsAddr", *metricsAddr,
		"probeAddr", *probeAddr)

	// Start hub manager (watches MetricCollectorReport on hub, queries Prometheus on member)
	klog.InfoS("Starting hub manager", "namespace", hubNamespace)
	return hubMgr.Start(ctx)
}
