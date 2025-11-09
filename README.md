# KubeFleet Cookbook

This repository features various labs, tutorials, and other learning materials that better help developers
explore KubeFleet's capabilities and how to integrate KubeFleet with other cloud-native projects in different
use cases.

KubeFleet is a sandbox project of the [Cloud Native Computing Foundation (CNCF)](https://cncf.io/) that
helps developers manage multiple Kubernetes clusters with ease. See also the
[KubeFleet repository](https://github.com/kubefleet-dev/kubefleet) for more information.

## Current projects

* [Multi-cluster LLM Inference with KAITO](/multi-cluster-ai-with-kaito)

    This tutorial showcases how to use KubeFleet with KAITO, a CNCF project that automates the AI/ML model inference
    or tuning workload in a Kubernetes cluster, to simplify multi-cluster multi-model inference tasks.

    Specifically, the tutorial uses:

    * KAITO, for running LLM based inference workloads in Kubernetes clusters;
    * Istio, for connecting clusters in a multi-cluster service mesh;
    * Kubernetes Gateway API with Inference Extension, for enabling self-hosted LLM query traffic;
    * KubeFleet, for managing KAITO, Istio, and Kubernetes Gateway API resources across clusters.

## Contributing

Help us make the KubeFleet cookbook better! To get started, see the [KubeFleet Cookbook Contributing Guide](CONTRIBUTING.md).

## Issues and Feedback

If you have any questions or concerns about the KubeFleet cookbook, any of its hosted tutorials, or this repository itself,
please raise a [GitHub issue](https://github.com/kubefleet-dev/cookbook/issues). You may also want to
[join the discussions](https://github.com/kubefleet-dev/cookbook/discussions).

Copyright The KubeFleet Authors. The Linux Foundation® (TLF) has registered trademarks and uses trademarks.
For a list of TLF trademarks, see [Trademark Usage](https://www.linuxfoundation.org/trademark-usage/).
=======
A collection of various demos, tutorials, and labs for using the KubeFleet project.

