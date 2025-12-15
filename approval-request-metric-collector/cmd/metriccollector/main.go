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
	"net/http"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet-cookbook/approval-request-metric-collector/apis/autoapprove/v1alpha1"
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

// buildHubConfig creates hub cluster config from environment variables
// following the same pattern as member-agent
func buildHubConfig() (*rest.Config, error) {
	hubURL := os.Getenv("HUB_SERVER_URL")
	if hubURL == "" {
		return nil, fmt.Errorf("HUB_SERVER_URL environment variable not set")
	}

	// Check for custom headers
	customHeader := os.Getenv("HUB_KUBE_HEADER")

	// Check TLS insecure flag
	tlsInsecure := os.Getenv("TLS_INSECURE") == "true"

	// Initialize hub config
	hubConfig := &rest.Config{
		Host: hubURL,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: tlsInsecure,
		},
		WrapTransport: func(rt http.RoundTripper) http.RoundTripper {
			if customHeader != "" {
				return &customHeaderTransport{
					Base:   rt,
					Header: customHeader,
				}
			}
			return rt
		},
	}

	// Check for certificate-based authentication
	identityKey := os.Getenv("IDENTITY_KEY")
	identityCert := os.Getenv("IDENTITY_CERT")
	if identityKey != "" && identityCert != "" {
		klog.InfoS("Using certificate-based authentication for hub cluster")
		// Read certificate files
		certData, err := os.ReadFile(identityCert)
		if err != nil {
			return nil, fmt.Errorf("failed to read identity cert: %w", err)
		}
		keyData, err := os.ReadFile(identityKey)
		if err != nil {
			return nil, fmt.Errorf("failed to read identity key: %w", err)
		}
		hubConfig.CertData = certData
		hubConfig.KeyData = keyData
	} else {
		// Token-based authentication
		klog.InfoS("Using token-based authentication for hub cluster")
		configPath := os.Getenv("CONFIG_PATH")
		if configPath == "" {
			configPath = "/var/run/secrets/hub/token"
		}
		tokenData, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read hub token from %s: %w", configPath, err)
		}
		hubConfig.BearerToken = string(tokenData)
	}

	// Handle CA certificate
	caBundle := os.Getenv("CA_BUNDLE")
	hubCA := os.Getenv("HUB_CERTIFICATE_AUTHORITY")
	if caBundle != "" {
		klog.InfoS("Using CA bundle for hub cluster TLS")
		caData, err := os.ReadFile(caBundle)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA bundle: %w", err)
		}
		hubConfig.CAData = caData
	} else if hubCA != "" {
		klog.InfoS("Using hub certificate authority for hub cluster TLS")
		caData, err := os.ReadFile(hubCA)
		if err != nil {
			return nil, fmt.Errorf("failed to read hub CA: %w", err)
		}
		hubConfig.CAData = caData
	} else {
		// If no CA specified, try to load system CA pool
		klog.InfoS("No CA specified, using insecure connection or system CA pool")
	}

	return hubConfig, nil
}

// customHeaderTransport adds custom headers to requests
type customHeaderTransport struct {
	Base   http.RoundTripper
	Header string
}

func (t *customHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Add("X-Custom-Header", t.Header)
	return t.Base.RoundTrip(req)
}

// Start starts the controller with hub cluster connection
func Start(ctx context.Context, hubCfg *rest.Config, memberClusterName, hubNamespace string) error {
	// Create scheme with required APIs
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add client-go scheme: %w", err)
	}
	if err := placementv1alpha1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add placement v1alpha1 API to scheme: %w", err)
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
