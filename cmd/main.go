/*
Copyright 2025.

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
	"flag"
	"os"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	gnmicv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	operatorv1alpha1 "github.com/gnmic/operator/api/v1alpha1"
	"github.com/gnmic/operator/internal/apiserver"
	"github.com/gnmic/operator/internal/controller"
	webhookv1alpha1 "github.com/gnmic/operator/internal/webhook/v1alpha1"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	// version is set at build time by the release workflow
	// (-ldflags "-X main.version=<tag>"); "dev" otherwise.
	version = "dev"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(gnmicv1alpha1.AddToScheme(scheme))
	utilruntime.Must(operatorv1alpha1.AddToScheme(scheme))
	utilruntime.Must(certmanagerv1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var devMode bool
	var apiAddr string
	var targetSourceConcurrency int
	var kubeAPIQPS float64
	var kubeAPIBurst int
	var watchNamespaces string
	flag.StringVar(&apiAddr, "api-bind-address", "", "The address the operator API endpoint binds to. Disabled if empty.")
	flag.BoolVar(&devMode, "dev-mode", false, "Enable development mode.")
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.IntVar(&targetSourceConcurrency, "targetsource-concurrency", controller.DefaultTargetSourceConcurrency,
		"How many TargetSources may run discovery at once. A run holds a worker for up to its spec.timeout, so this bounds how long a slow source can delay the others.")
	flag.Float64Var(&kubeAPIQPS, "kube-api-qps", 50, "Maximum sustained queries per second to the Kubernetes API server. The client-go default (20) is too low for large target populations.")
	flag.IntVar(&kubeAPIBurst, "kube-api-burst", 100, "Maximum burst of queries to the Kubernetes API server.")
	flag.StringVar(&watchNamespaces, "watch-namespaces", "", "Comma-separated list of namespaces to watch. Empty (the default) watches all namespaces, which caches every Secret, ConfigMap, Service, StatefulSet and Certificate in the cluster.")
	opts := zap.Options{
		Development: devMode,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// The rest-client defaults (20 QPS / 30 burst) throttle every controller in the
	// process behind whichever one is busiest. With a few thousand Targets that shows up
	// as inexplicably slow Cluster reconciles rather than as an obvious error.
	restConfig := ctrl.GetConfigOrDie()
	restConfig.QPS = float32(kubeAPIQPS)
	restConfig.Burst = kubeAPIBurst
	setupLog.Info("configured Kubernetes API client rate limits", "qps", restConfig.QPS, "burst", restConfig.Burst)

	// Restricting the cache to specific namespaces is the single largest reduction in
	// informer footprint available: the manager caches every Secret, ConfigMap, Service,
	// StatefulSet and Certificate it touches, cluster-wide, and Secrets in particular are
	// unbounded and unrelated to Target count.
	//
	// This is safe to scope because every resolution path is already namespace-local — a
	// Pipeline only ever resolves Targets, Subscriptions, Outputs, Inputs and Processors in
	// its own namespace, and credentials are read from the Target's namespace. Nothing is
	// read from the operator's own namespace: its TLS material comes from mounted files, not
	// the API.
	namespaces := parseWatchNamespaces(watchNamespaces)
	cacheOpts := cache.Options{}
	if len(namespaces) > 0 {
		cacheOpts.DefaultNamespaces = make(map[string]cache.Config, len(namespaces))
		for _, ns := range namespaces {
			cacheOpts.DefaultNamespaces[ns] = cache.Config{}
		}
		setupLog.Info("restricting cache to namespaces", "namespaces", namespaces)
	} else {
		setupLog.Info("watching all namespaces; set --watch-namespaces to reduce cache footprint")
	}
	// The webhooks are registered cluster-wide, so they receive admission requests for
	// namespaces this instance does not reconcile. Give them the same list so they can warn
	// rather than silently accept resources that will never be acted on.
	webhookv1alpha1.SetWatchedNamespaces(namespaces)

	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:                 scheme,
		Cache:                  cacheOpts,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "47942d33.gnmic.dev",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	// Shared between the Cluster and TargetState controllers: the first records
	// what it applied to each pod, the second invalidates a pod's record when
	// its SSE stream drops, which is the operator's only sign that a pod may
	// have restarted and lost its configuration.
	applyCache := controller.NewApplyCache()

	// The operator's own client certificate and CA, watched on disk. Rotation is
	// picked up by fsnotify rather than by re-reading the files on every reconcile,
	// and the certificate is resolved per TLS handshake, so a new one applies
	// without rebuilding any client.
	tlsMaterial := controller.NewTLSMaterial()
	if err := mgr.Add(tlsMaterial); err != nil {
		setupLog.Error(err, "unable to start the TLS material watcher")
		os.Exit(1)
	}

	clusterReconciler := &controller.ClusterReconciler{
		Client:  mgr.GetClient(),
		Scheme:  mgr.GetScheme(),
		Applied: applyCache,
		TLS:     tlsMaterial,
	}
	if err = clusterReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Cluster")
		os.Exit(1)
	}
	if err = (&controller.PipelineReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Pipeline")
		os.Exit(1)
	}

	// The API server runs on every replica, leader or not. Its refresh endpoint
	// only needs a client, and a Service in front of several replicas must not
	// refuse requests that land on a follower.
	var api *apiserver.APIServer
	if apiAddr != "" {
		api = apiserver.New(apiAddr, clusterReconciler, mgr.GetClient())
	}
	if err := (&controller.TargetSourceReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		Recorder:    mgr.GetEventRecorderFor("targetsource-controller"),
		Concurrency: targetSourceConcurrency,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "TargetSource")
		os.Exit(1)
	}
	// nolint:goconst
	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		if err := webhookv1alpha1.SetupTargetSourceWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "TargetSource")
			os.Exit(1)
		}
	}
	// nolint:goconst
	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		if err := webhookv1alpha1.SetupClusterWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "Cluster")
			os.Exit(1)
		}
	}
	// nolint:goconst
	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		if err := webhookv1alpha1.SetupPipelineWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "Pipeline")
			os.Exit(1)
		}
	}
	// nolint:goconst
	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		if err := webhookv1alpha1.SetupSubscriptionWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "Subscription")
			os.Exit(1)
		}
	}
	// nolint:goconst
	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		if err := webhookv1alpha1.SetupTargetWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "Target")
			os.Exit(1)
		}
	}
	// nolint:goconst
	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		if err := webhookv1alpha1.SetupOutputWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "Output")
			os.Exit(1)
		}
	}
	// nolint:goconst
	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		if err := webhookv1alpha1.SetupInputWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "Input")
			os.Exit(1)
		}
	}
	// nolint:goconst
	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		if err := webhookv1alpha1.SetupProcessorWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "Processor")
			os.Exit(1)
		}
	}
	// nolint:goconst
	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		if err := webhookv1alpha1.SetupTargetProfileWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "TargetProfile")
			os.Exit(1)
		}
	}
	if err = (&controller.TargetStateReconciler{
		Client:  mgr.GetClient(),
		Scheme:  mgr.GetScheme(),
		Applied: applyCache,
		TLS:     tlsMaterial,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "TargetState")
		os.Exit(1)
	}
	// nolint:goconst
	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		if err := webhookv1alpha1.SetupTunnelTargetPolicyWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "TunnelTargetPolicy")
			os.Exit(1)
		}
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	if api != nil {
		if err := mgr.Add(api); err != nil {
			setupLog.Error(err, "unable to add api server")
			os.Exit(1)
		}
	}

	// start manager
	setupLog.Info("starting manager", "version", version)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// parseWatchNamespaces splits the --watch-namespaces value into a deduplicated, ordered
// list. An empty or whitespace-only value yields nil, meaning "watch all namespaces".
func parseWatchNamespaces(value string) []string {
	seen := make(map[string]struct{})
	var namespaces []string
	for _, ns := range strings.Split(value, ",") {
		ns = strings.TrimSpace(ns)
		if ns == "" {
			continue
		}
		if _, ok := seen[ns]; ok {
			continue
		}
		seen[ns] = struct{}{}
		namespaces = append(namespaces, ns)
	}
	return namespaces
}
