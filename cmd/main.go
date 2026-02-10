/*
Copyright 2026.

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
	"crypto/tls"
	"flag"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	assistv1alpha1 "github.com/osagberg/kube-assist-operator/api/v1alpha1"
	"github.com/osagberg/kube-assist-operator/internal/ai"
	"github.com/osagberg/kube-assist-operator/internal/causal"
	"github.com/osagberg/kube-assist-operator/internal/checker"
	"github.com/osagberg/kube-assist-operator/internal/checker/flux"
	"github.com/osagberg/kube-assist-operator/internal/checker/resource"
	"github.com/osagberg/kube-assist-operator/internal/checker/workload"
	"github.com/osagberg/kube-assist-operator/internal/controller"
	"github.com/osagberg/kube-assist-operator/internal/dashboard"
	"github.com/osagberg/kube-assist-operator/internal/datasource"
	"github.com/osagberg/kube-assist-operator/internal/notifier"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(assistv1alpha1.AddToScheme(scheme))

	// Flux CRDs for health checking
	utilruntime.Must(helmv2.AddToScheme(scheme))
	utilruntime.Must(kustomizev1.AddToScheme(scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// nolint:gocyclo
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var enableWebhooks bool
	var enableDashboard bool
	var dashboardAddr string
	var dashboardTLSCertFile string
	var dashboardTLSKeyFile string
	var enableAI bool
	var aiProvider string
	var aiAPIKey string
	var aiModel string
	var aiExplainModel string
	var aiDailyTokenLimit int
	var aiMonthlyTokenLimit int
	var maxSSEClients int
	var notifySemCapacity int
	var datasourceType string
	var consoleURL string
	var clusterID string
	var consoleBearerToken string
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.BoolVar(&enableWebhooks, "enable-webhooks", false,
		"Enable admission webhooks for CRD validation.")
	flag.BoolVar(&enableDashboard, "enable-dashboard", false,
		"Enable the Team Health Dashboard web server.")
	flag.StringVar(&dashboardAddr, "dashboard-bind-address", ":9090",
		"The address the dashboard server binds to.")
	flag.StringVar(&dashboardTLSCertFile, "dashboard-tls-cert-file", "",
		"Path to TLS certificate file for dashboard HTTPS.")
	flag.StringVar(&dashboardTLSKeyFile, "dashboard-tls-key-file", "",
		"Path to TLS key file for dashboard HTTPS.")
	flag.BoolVar(&enableAI, "enable-ai", false,
		"Enable AI-powered suggestions for health check issues.")
	flag.StringVar(&aiProvider, "ai-provider", "noop",
		"AI provider to use: anthropic, openai, or noop (default: noop).")
	flag.StringVar(&aiAPIKey, "ai-api-key", "",
		"API key for AI provider (or use KUBE_ASSIST_AI_API_KEY env var).")
	flag.StringVar(&aiModel, "ai-model", "",
		"AI model to use (provider default if empty).")
	flag.StringVar(&aiExplainModel, "ai-explain-model", "",
		"Optional cheaper model for AI explain/narrative mode (uses primary model if empty).")
	flag.IntVar(&aiDailyTokenLimit, "ai-daily-token-limit", 0,
		"Max AI tokens per day (0 = unlimited).")
	flag.IntVar(&aiMonthlyTokenLimit, "ai-monthly-token-limit", 0,
		"Max AI tokens per month (0 = unlimited).")
	flag.IntVar(&maxSSEClients, "max-sse-clients", 100,
		"Maximum concurrent SSE dashboard connections (0 = unlimited).")
	flag.IntVar(&notifySemCapacity, "notify-sem-capacity", 5,
		"Maximum concurrent notification dispatches.")
	flag.StringVar(&datasourceType, "datasource", "kubernetes",
		"DataSource backend: kubernetes or console.")
	flag.StringVar(&consoleURL, "console-url", "",
		"Console backend URL (required when --datasource=console).")
	flag.StringVar(&clusterID, "cluster-id", "",
		"Cluster identifier for console backend.")
	flag.StringVar(&consoleBearerToken, "console-bearer-token", "",
		"Bearer token for authenticating with console backend.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Only create webhook server when webhooks are enabled
	var webhookServer webhook.Server
	if enableWebhooks {
		webhookTLSOpts := tlsOpts
		webhookServerOptions := webhook.Options{
			TLSOpts: webhookTLSOpts,
		}

		if len(webhookCertPath) > 0 {
			setupLog.Info("Initializing webhook certificate watcher using provided certificates",
				"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

			webhookServerOptions.CertDir = webhookCertPath
			webhookServerOptions.CertName = webhookCertName
			webhookServerOptions.KeyName = webhookCertKey
		}

		webhookServer = webhook.NewServer(webhookServerOptions)
	}

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.1/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.1/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	mgrOptions := ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "12e90909.cluster.local",
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
	}
	if enableWebhooks {
		mgrOptions.WebhookServer = webhookServer
	}
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOptions)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	var ds datasource.DataSource
	switch datasourceType {
	case "kubernetes":
		ds = datasource.NewKubernetes(mgr.GetClient())
	case "console":
		if consoleURL == "" {
			setupLog.Error(nil, "--console-url is required when --datasource=console")
			os.Exit(1)
		}
		if err := datasource.ValidateConsoleURL(consoleURL); err != nil {
			setupLog.Error(err, "invalid --console-url")
			os.Exit(1)
		}
		if clusterID == "" {
			setupLog.Error(nil, "--cluster-id is required when --datasource=console")
			os.Exit(1)
		}
		if err := datasource.ValidateClusterID(clusterID); err != nil {
			setupLog.Error(err, "invalid --cluster-id")
			os.Exit(1)
		}
		var consoleOpts []datasource.ConsoleOption
		// Flag takes precedence; fall back to env var (set by Helm secret ref).
		if consoleBearerToken == "" {
			consoleBearerToken = os.Getenv("CONSOLE_BEARER_TOKEN")
		}
		if consoleBearerToken != "" {
			consoleOpts = append(consoleOpts, datasource.WithBearerToken(consoleBearerToken))
		}
		cds, err := datasource.NewConsole(consoleURL, clusterID, consoleOpts...)
		if err != nil {
			setupLog.Error(err, "failed to create console datasource")
			os.Exit(1)
		}
		ds = cds
		setupLog.Info("Using console datasource", "url", consoleURL, "cluster", clusterID)
	default:
		setupLog.Error(nil, "unknown --datasource value", "datasource", datasourceType)
		os.Exit(1)
	}

	// Create Kubernetes clientset for pod logs access
	clientset, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to create kubernetes clientset")
		os.Exit(1)
	}

	// Initialize checker registry with available checkers
	registry := checker.NewRegistry()
	registry.MustRegister(workload.New())
	registry.MustRegister(flux.NewHelmReleaseChecker())
	registry.MustRegister(flux.NewKustomizationChecker())
	registry.MustRegister(flux.NewGitRepositoryChecker())
	registry.MustRegister(resource.NewSecretChecker())
	registry.MustRegister(resource.NewPVCChecker())
	registry.MustRegister(resource.NewQuotaChecker())
	registry.MustRegister(resource.NewNetworkPolicyChecker())
	setupLog.Info("Registered checkers", "checkers", registry.List())

	// Initialize notifier registry
	notifierRegistry := notifier.NewRegistry()

	// Initialize AI provider with runtime-reconfigurable Manager
	var aiConfig ai.Config
	if enableAI {
		aiConfig = ai.Config{
			Provider:          aiProvider,
			APIKey:            aiAPIKey,
			Model:             aiModel,
			ExplainModel:      aiExplainModel,
			DailyTokenLimit:   aiDailyTokenLimit,
			MonthlyTokenLimit: aiMonthlyTokenLimit,
		}
		// Fall back to environment variables for individual fields not set via flags
		envConfig := ai.ConfigFromEnv()
		if aiConfig.APIKey == "" {
			aiConfig.APIKey = envConfig.APIKey
		}
		if aiConfig.Model == "" {
			aiConfig.Model = envConfig.Model
		}
	} else {
		aiConfig = ai.DefaultConfig()
	}
	fast, explain, budget, aiErr := ai.NewProvider(aiConfig)
	if aiErr != nil {
		setupLog.Error(aiErr, "failed to create AI provider")
		os.Exit(1)
	}
	cache := ai.NewCache(100, 5*time.Minute, enableAI)
	aiManager := ai.NewManager(fast, explain, enableAI, budget, cache)
	setupLog.Info("AI provider initialized",
		"provider", aiManager.Name(),
		"enabled", enableAI,
		"available", aiManager.Available(),
		"explainModel", aiExplainModel,
		"dailyTokenLimit", aiDailyTokenLimit,
		"monthlyTokenLimit", aiMonthlyTokenLimit)

	if err := (&controller.TroubleshootRequestReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Clientset: clientset,
		Registry:  registry,
		Recorder:  mgr.GetEventRecorder("troubleshootrequest-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "TroubleshootRequest")
		os.Exit(1)
	}

	if err := (&controller.TeamHealthRequestReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		Registry:         registry,
		AIProvider:       aiManager,
		DataSource:       ds,
		Correlator:       causal.NewCorrelator(),
		NotifierRegistry: notifierRegistry,
		NotifySem:        make(chan struct{}, notifySemCapacity),
		Recorder:         mgr.GetEventRecorder("teamhealthrequest-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "TeamHealthRequest")
		os.Exit(1)
	}
	if err := (&controller.CheckPluginReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Registry: registry,
		Recorder: mgr.GetEventRecorder("checkplugin-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CheckPlugin")
		os.Exit(1)
	}

	if enableWebhooks {
		if err := assistv1alpha1.SetupTroubleshootRequestWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "TroubleshootRequest")
			os.Exit(1)
		}
		if err := assistv1alpha1.SetupTeamHealthRequestWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "TeamHealthRequest")
			os.Exit(1)
		}
		if err := assistv1alpha1.SetupCheckPluginWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "CheckPlugin")
			os.Exit(1)
		}
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// Create shared signal handler context (can only be called once)
	ctx := ctrl.SetupSignalHandler()

	// Start dashboard server if enabled
	if enableDashboard {
		dashboardServer := dashboard.NewServer(ds, registry, dashboardAddr).
			WithAI(aiManager, enableAI).
			WithMaxSSEClients(maxSSEClients)
		if datasourceType == "kubernetes" {
			dashboardServer = dashboardServer.WithK8sWriter(mgr.GetClient(), mgr.GetScheme())
		}
		if dashboardTLSCertFile != "" && dashboardTLSKeyFile != "" {
			dashboardServer.WithTLS(dashboardTLSCertFile, dashboardTLSKeyFile)
		}
		go func() {
			setupLog.Info("starting dashboard server", "addr", dashboardAddr)
			if err := dashboardServer.Start(ctx); err != nil {
				setupLog.Error(err, "dashboard server error")
			}
		}()
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
