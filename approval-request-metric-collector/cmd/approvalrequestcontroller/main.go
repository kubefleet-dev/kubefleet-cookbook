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

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	autoapprovev1alpha1 "github.com/kubefleet-dev/kubefleet-cookbook/approval-request-metric-collector/apis/autoapprove/v1alpha1"
	approvalcontroller "github.com/kubefleet-dev/kubefleet-cookbook/approval-request-metric-collector/pkg/controllers/approvalrequest"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(placementv1beta1.AddToScheme(scheme))
	utilruntime.Must(autoapprovev1alpha1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var probeAddr string

	// Add klog flags to support -v for verbosity
	klog.InitFlags(nil)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	klog.InfoS("Starting ApprovalRequest Controller")

	config := ctrl.GetConfigOrDie()

	// Check required CRDs are installed before starting
	if err := checkRequiredCRDs(config); err != nil {
		klog.ErrorS(err, "Required CRDs not found")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
	})
	if err != nil {
		klog.ErrorS(err, "Unable to create manager")
		os.Exit(1)
	}

	// Setup ApprovalRequest controller
	approvalRequestReconciler := &approvalcontroller.Reconciler{
		Client: mgr.GetClient(),
	}
	if err = approvalRequestReconciler.SetupWithManagerForApprovalRequest(mgr); err != nil {
		klog.ErrorS(err, "Unable to create controller", "controller", "ApprovalRequest")
		os.Exit(1)
	}

	// Setup ClusterApprovalRequest controller
	clusterApprovalRequestReconciler := &approvalcontroller.Reconciler{
		Client: mgr.GetClient(),
	}
	if err = clusterApprovalRequestReconciler.SetupWithManagerForClusterApprovalRequest(mgr); err != nil {
		klog.ErrorS(err, "Unable to create controller", "controller", "ClusterApprovalRequest")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Unable to set up health check")
		os.Exit(1)
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Unable to set up ready check")
		os.Exit(1)
	}

	klog.InfoS("Starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		klog.ErrorS(err, "Problem running manager")
		os.Exit(1)
	}
}

// checkRequiredCRDs checks that all required CRDs are installed
func checkRequiredCRDs(config *rest.Config) error {
	requiredCRDs := []string{
		"approvalrequests.placement.kubernetes-fleet.io",
		"clusterapprovalrequests.placement.kubernetes-fleet.io",
		"metriccollectorreports.autoapprove.kubernetes-fleet.io",
		"clusterstagedworkloadtrackers.autoapprove.kubernetes-fleet.io",
		"stagedworkloadtrackers.autoapprove.kubernetes-fleet.io",
		"clusterstagedupdateruns.placement.kubernetes-fleet.io",
		"stagedupdateruns.placement.kubernetes-fleet.io",
	}

	klog.InfoS("Checking for required CRDs", "count", len(requiredCRDs))

	c, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}

	ctx := context.Background()
	missingCRDs := []string{}

	for _, crdName := range requiredCRDs {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		err := c.Get(ctx, client.ObjectKey{Name: crdName}, crd)
		if err != nil {
			klog.ErrorS(err, "CRD not found", "crd", crdName)
			missingCRDs = append(missingCRDs, crdName)
		} else {
			klog.V(3).InfoS("CRD found", "crd", crdName)
		}
	}

	if len(missingCRDs) > 0 {
		return fmt.Errorf("missing required CRDs: %v", missingCRDs)
	}

	klog.InfoS("All required CRDs are installed")
	return nil
}
