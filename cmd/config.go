package main

import (
	"crypto/tls"
	"errors"
	"strings"
	"time"

	"github.com/SigNoz/signoz-operator/internal/instrumentation"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

type config struct {
	LogLevel                      string
	MetricsBindAddress            string
	HealthProbeBindAddress        string
	LeaderElect                   bool
	MetricsSecure                 bool
	WebhookCertPath               string
	WebhookCertName               string
	WebhookCertKey                string
	MetricsCertPath               string
	MetricsCertName               string
	MetricsCertKey                string
	EnableHTTP2                   bool
	DefaultResourcesInterval      time.Duration
	DefaultResourcesRetryInterval time.Duration
	DefaultResourcesTimeout       time.Duration
	WatchNamespaces               []string
	OperatorNamespace             string
}

func (c *config) RegisterFlags(cmd *cobra.Command) {
	flags := cmd.PersistentFlags()

	flags.StringVar(&c.LogLevel, "log-level", "info", "The level of the logger, Can be one of 'debug', 'info', 'error', 'panic'.")
	flags.StringVar(&c.MetricsBindAddress, "metrics-bind-address", "0", "The address the metrics endpoint binds to. Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flags.StringVar(&c.HealthProbeBindAddress, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flags.BoolVar(&c.LeaderElect, "leader-elect", false, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flags.BoolVar(&c.MetricsSecure, "metrics-secure", true, "If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flags.StringVar(&c.WebhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flags.StringVar(&c.WebhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flags.StringVar(&c.WebhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flags.StringVar(&c.MetricsCertPath, "metrics-cert-path", "", "The directory that contains the metrics server certificate.")
	flags.StringVar(&c.MetricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flags.StringVar(&c.MetricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flags.BoolVar(&c.EnableHTTP2, "enable-http2", false, "If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flags.DurationVar(&c.DefaultResourcesInterval, "default-resources-interval", 10*time.Minute, "Interval at which resources are reconciled against SigNoz. Applicable only when the resource spec omits .spec.interval.")
	flags.DurationVar(&c.DefaultResourcesRetryInterval, "default-resources-retry-interval", time.Minute, "Interval at which a recoverable failure is retried. Applicable only when the resource spec omits .spec.retryInterval.")
	flags.DurationVar(&c.DefaultResourcesTimeout, "default-resources-timeout", 30*time.Second, "Upper bound on a single reconciliation attempt when a resource's spec omits one. This includes the time taken by the SigNoz API.")
	flags.StringSliceVar(&c.WatchNamespaces, "watch-namespaces", nil, "The namespace(s) the manager should watch for changes. Defaults to watching all namespaces.")
	flags.StringVar(&c.OperatorNamespace, "operator-namespace", "", "Namespace in which the operator is running. A ClusterProviderConfig's Secret and ConfigMap references resolve here.")
}

// buildConfig validates the resolved configuration, flags and environment
// alike, before the manager builds a client. Requiredness is not delegated to
// cobra, which validates before the hook that reads the environment has run.
func (c *config) buildConfig() error {
	if err := c.buildLogger(); err != nil {
		return err
	}

	if strings.TrimSpace(c.OperatorNamespace) == "" {
		return errors.New("--operator-namespace is required: a ClusterProviderConfig's Secret and ConfigMap references resolve there")
	}

	return nil
}

func (c *config) buildWebhookServerOptions() webhook.Options {
	// if the enable-http2 flag is false (the default), http/2 should be disabled due to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs.
	var tlsOpts []func(*tls.Config)

	if !c.EnableHTTP2 {
		tlsOpts = append(tlsOpts, func(tlsCfg *tls.Config) {
			setupLog.Info("Disabling http/2 in the webhook server")
			tlsCfg.NextProtos = []string{"http/1.1"}
		})
	}

	webhookServerOptions := webhook.Options{
		TLSOpts: tlsOpts,
	}

	if len(c.WebhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates", "webhookCertPath", c.WebhookCertPath, "webhookCertName", c.WebhookCertName, "webhookCertKey", c.WebhookCertKey)

		webhookServerOptions.CertDir = c.WebhookCertPath
		webhookServerOptions.CertName = c.WebhookCertName
		webhookServerOptions.KeyName = c.WebhookCertKey
	}

	return webhookServerOptions
}

func (c *config) buildMetricsServerOptions() metricsserver.Options {
	// if the enable-http2 flag is false (the default), http/2 should be disabled due to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs.
	var tlsOpts []func(*tls.Config)

	if !c.EnableHTTP2 {
		tlsOpts = append(tlsOpts, func(tlsCfg *tls.Config) {
			setupLog.Info("Disabling http/2 in the metrics server")
			tlsCfg.NextProtos = []string{"http/1.1"}
		})
	}

	metricsServerOptions := metricsserver.Options{
		BindAddress:   c.MetricsBindAddress,
		SecureServing: c.MetricsSecure,
		TLSOpts:       tlsOpts,
	}

	if len(c.MetricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates", "metricsCertPath", c.MetricsCertPath, "metricsCertName", c.MetricsCertName, "metricsCertKey", c.MetricsCertKey)

		metricsServerOptions.CertDir = c.MetricsCertPath
		metricsServerOptions.CertName = c.MetricsCertName
		metricsServerOptions.KeyName = c.MetricsCertKey
	}

	return metricsServerOptions
}

func (c *config) buildCacheOptions() cache.Options {
	if len(c.WatchNamespaces) == 0 {
		return cache.Options{}
	}

	namespaces := make(map[string]cache.Config, len(c.WatchNamespaces)+1)

	for _, ns := range c.WatchNamespaces {
		if ns = strings.TrimSpace(ns); ns != "" {
			namespaces[ns] = cache.Config{}
		}
	}

	namespaces[c.OperatorNamespace] = cache.Config{}
	setupLog.Info("Watching namespace(s)", "namespaces", c.WatchNamespaces, "operatorNamespace", c.OperatorNamespace)

	return cache.Options{DefaultNamespaces: namespaces}
}

func (c *config) buildLogger() error {
	logger, err := instrumentation.NewLoggerWithZap(c.LogLevel)
	if err != nil {
		return err
	}

	ctrl.SetLogger(logger)
	return nil
}
