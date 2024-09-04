/*
Copyright 2022 The Kubernetes Authors.

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
	"time"

	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	cgrecord "k8s.io/client-go/tools/record"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	logsv1 "k8s.io/component-base/logs/api/v1"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/flags"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	infrav1b1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta1"
	infrav1b2 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta2"
	infrav1b3 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/controllers"
	"sigs.k8s.io/cluster-api-provider-cloudstack/controllers/utils"
	"sigs.k8s.io/cluster-api-provider-cloudstack/version"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(infrav1b1.AddToScheme(scheme))
	utilruntime.Must(infrav1b2.AddToScheme(scheme))
	utilruntime.Must(infrav1b3.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

var (
	enableLeaderElection        bool
	leaderElectionLeaseDuration time.Duration
	leaderElectionRenewDeadline time.Duration
	leaderElectionRetryPeriod   time.Duration
	leaderElectionNamespace     string
	watchNamespace              string
	watchFilterValue            string
	profilerAddr                string
	metricsAddr                 string
	probeAddr                   string
	syncPeriod                  time.Duration
	webhookCertDir              string
	webhookPort                 int
	showVersion                 bool

	cloudStackClusterConcurrency       int
	cloudStackMachineConcurrency       int
	cloudStackAffinityGroupConcurrency int
	cloudStackFailureDomainConcurrency int

	tlsOptions = flags.TLSOptions{}
	logOptions = logs.NewOptions()
)

func initFlags(fs *pflag.FlagSet) {
	fs.StringVar(
		&metricsAddr,
		"metrics-bind-addr",
		"localhost:8080",
		"The address the metric endpoint binds to.")
	fs.StringVar(
		&probeAddr,
		"health-probe-bind-address",
		":8081",
		"The address the probe endpoint binds to.")
	fs.BoolVar(
		&enableLeaderElection,
		"leader-elect",
		false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	fs.DurationVar(&leaderElectionLeaseDuration, "leader-elect-lease-duration", 15*time.Second,
		"Interval at which non-leader candidates will wait to force acquire leadership (duration string)")

	fs.DurationVar(&leaderElectionRenewDeadline, "leader-elect-renew-deadline", 10*time.Second,
		"Duration that the leading controller manager will retry refreshing leadership before giving up (duration string)")

	fs.DurationVar(&leaderElectionRetryPeriod, "leader-elect-retry-period", 2*time.Second,
		"Duration the LeaderElector clients should wait between tries of actions (duration string)")
	fs.StringVar(
		&watchNamespace,
		"namespace",
		"",
		"Namespace that the controller watches to reconcile cluster-api objects. If unspecified, "+
			"the controller watches for cluster-api objects across all namespaces.")
	fs.StringVar(
		&watchFilterValue,
		"watch-filter",
		"",
		fmt.Sprintf(
			"Label value that the controller watches to reconcile cluster-api objects. "+
				"Label key is always %s. If unspecified, the controller watches for all cluster-api objects.",
			clusterv1.WatchLabel))
	fs.StringVar(
		&leaderElectionNamespace,
		"leader-elect-namespace",
		"",
		"Namespace that the controller performs leader election in. If unspecified, the controller will discover which namespace it is running in.",
	)
	fs.StringVar(
		&profilerAddr,
		"profiler-address",
		"",
		"Bind address to expose the pprof profiler (e.g. localhost:6060)")
	fs.IntVar(
		&webhookPort,
		"webhook-port",
		9443,
		"The webhook server port the manager will listen on.")
	fs.StringVar(
		&webhookCertDir,
		"webhook-cert-dir",
		"/tmp/k8s-webhook-server/serving-certs/",
		"Specify the directory where webhooks will get tls certificates.")
	fs.IntVar(
		&cloudStackClusterConcurrency,
		"cloudstackcluster-concurrency",
		10,
		"Maximum concurrent reconciles for CloudStackCluster resources",
	)
	fs.IntVar(
		&cloudStackMachineConcurrency,
		"cloudstackmachine-concurrency",
		10,
		"Maximum concurrent reconciles for CloudStackMachine resources",
	)
	fs.IntVar(
		&cloudStackAffinityGroupConcurrency,
		"cloudstackaffinitygroup-concurrency",
		5,
		"Maximum concurrent reconciles for CloudStackAffinityGroup resources",
	)
	fs.IntVar(
		&cloudStackFailureDomainConcurrency,
		"cloudstackfailuredomain-concurrency",
		5,
		"Maximum concurrent reconciles for CloudStackFailureDomain resources",
	)
	fs.DurationVar(&syncPeriod,
		"sync-period",
		10*time.Minute,
		"The minimum interval at which watched resources are reconciled",
	)
	fs.BoolVar(&showVersion, "version", false, "Show current version and exit.")

	logs.AddFlags(fs, logs.SkipLoggingConfigurationFlags())
	logsv1.AddFlags(logOptions, fs)

	flags.AddTLSOptions(fs, &tlsOptions)
}

func main() {
	initFlags(pflag.CommandLine)
	pflag.CommandLine.SetNormalizeFunc(cliflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine) // Merge klog's goflag flags into the pflags.
	pflag.Parse()

	if showVersion {
		fmt.Println(version.Get().String()) //nolint:forbidigo
		os.Exit(0)
	}

	if err := logsv1.ValidateAndApply(logOptions, nil); err != nil {
		setupLog.Error(err, "unable to start manager")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	ctrl.SetLogger(klog.Background())

	if profilerAddr != "" {
		klog.Infof("Profiler listening for requests at %s", profilerAddr)
		go func() {
			klog.Info(http.ListenAndServe(profilerAddr, nil)) //nolint:gosec
		}()
	}

	tlsOptionOverrides, err := flags.GetTLSOptionOverrideFuncs(tlsOptions)
	if err != nil {
		setupLog.Error(err, "unable to add TLS settings to the webhook server")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	var watchNamespaces []string
	if watchNamespace != "" {
		setupLog.Info("Watching cluster-api objects only in namespace for reconciliation", "namespace", watchNamespace)
		watchNamespaces = []string{watchNamespace}
	}

	// Machine and cluster operations can create enough events to trigger the event recorder spam filter
	// Setting the burst size higher ensures all events will be recorded and submitted to the API
	broadcaster := cgrecord.NewBroadcasterWithCorrelatorOptions(cgrecord.CorrelatorOptions{
		BurstSize: 100,
	})

	// Define user agent for the controller
	restConfig := ctrl.GetConfigOrDie()
	restConfig.UserAgent = "cluster-api-provider-cloudstack-controller"

	// Create the controller manager.
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                  scheme,
		MetricsBindAddress:      metricsAddr,
		HealthProbeBindAddress:  probeAddr,
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "capc-leader-election-controller",
		LeaderElectionNamespace: leaderElectionNamespace,
		LeaseDuration:           &leaderElectionLeaseDuration,
		RenewDeadline:           &leaderElectionRenewDeadline,
		RetryPeriod:             &leaderElectionRetryPeriod,
		PprofBindAddress:        profilerAddr,
		Cache: cache.Options{
			Namespaces: watchNamespaces,
			SyncPeriod: &syncPeriod,
		},
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{
					&corev1.ConfigMap{},
					&corev1.Secret{},
				},
			},
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    webhookPort,
			CertDir: webhookCertDir,
			TLSOpts: tlsOptionOverrides,
		}),
		EventBroadcaster: broadcaster,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	// Register reconcilers with the controller manager.
	base := utils.ReconcilerBase{
		K8sClient:        mgr.GetClient(),
		Recorder:         mgr.GetEventRecorderFor("capc-controller-manager"),
		Scheme:           mgr.GetScheme(),
		WatchFilterValue: watchFilterValue,
	}

	ctx := ctrl.SetupSignalHandler()
	setupReconcilers(ctx, base, mgr)
	infrav1b3.K8sClient = base.K8sClient

	// +kubebuilder:scaffold:builder

	// Add health and ready checks.
	if err = mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	if err = mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	// Start the controller manager.
	if err = (&infrav1b3.CloudStackCluster{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "CloudStackCluster")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	if err = (&infrav1b3.CloudStackMachine{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "CloudStackMachine")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	if err = (&infrav1b3.CloudStackMachineTemplate{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "CloudStackMachineTemplate")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	setupLog.Info("starting manager", "version", version.Get().String())
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
}

func setupReconcilers(ctx context.Context, base utils.ReconcilerBase, mgr manager.Manager) {
	if err := (&controllers.CloudStackClusterReconciler{ReconcilerBase: base}).SetupWithManager(ctx, mgr, controller.Options{MaxConcurrentReconciles: cloudStackClusterConcurrency}); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CloudStackCluster")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	if err := (&controllers.CloudStackMachineReconciler{ReconcilerBase: base}).SetupWithManager(ctx, mgr, controller.Options{MaxConcurrentReconciles: cloudStackMachineConcurrency}); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CloudStackMachine")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	if err := (&controllers.CloudStackIsoNetReconciler{ReconcilerBase: base}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CloudStackIsoNetReconciler")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	if err := (&controllers.CloudStackAffinityGroupReconciler{ReconcilerBase: base}).SetupWithManager(ctx, mgr, controller.Options{MaxConcurrentReconciles: cloudStackAffinityGroupConcurrency}); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CloudStackAffinityGroup")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	if err := (&controllers.CloudStackFailureDomainReconciler{ReconcilerBase: base}).SetupWithManager(ctx, mgr, controller.Options{MaxConcurrentReconciles: cloudStackFailureDomainConcurrency}); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CloudStackFailureDomain")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
}
