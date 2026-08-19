package main

import (
	"errors"
	"fmt"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/SigNoz/signoz-operator/internal/build"
	internalconfig "github.com/SigNoz/signoz-operator/internal/config"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	resourcescontroller "github.com/SigNoz/signoz-operator/internal/controller/resources"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(resourcesv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	cfg := &config{}

	cmd := &cobra.Command{
		Use:          "signoz-operator",
		Version:      build.Version,
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return internalconfig.Set(cmd, "signoz-operator")
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return run(cfg)
		},
	}

	cfg.RegisterFlags(cmd)

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cfg *config) error {
	if err := cfg.buildLogger(); err != nil {
		return err
	}

	if cfg.OperatorNamespace == "" {
		return errors.New("--operator-namespace is required: a ClusterProviderConfig's Secret and ConfigMap references resolve there")
	}

	webhookServer := webhook.NewServer(cfg.buildWebhookServerOptions())

	mgrOptions := ctrl.Options{
		Scheme:                 scheme,
		Metrics:                cfg.buildMetricsServerOptions(),
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: cfg.HealthProbeBindAddress,
		LeaderElection:         cfg.LeaderElect,
		LeaderElectionID:       "1f9508b2.signoz.io",
		Cache:                  cfg.buildCacheOptions(),
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOptions)
	if err != nil {
		return fmt.Errorf("could not create manager: %w", err)
	}

	if err := (&resourcescontroller.ProviderConfigReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("could not create controller resources-providerconfig: %w", err)
	}

	if err := (&resourcescontroller.ClusterProviderConfigReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Namespace: cfg.OperatorNamespace,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("could not create controller resources-clusterproviderconfig: %w", err)
	}

	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("could not set up health check: %w", err)
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("could not set up ready check: %w", err)
	}

	setupLog.Info("Starting manager", "version", build.Version)

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("manager exited: %w", err)
	}

	return nil
}
