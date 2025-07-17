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
	goruntime "runtime"
	"time"

	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	logsv1 "k8s.io/component-base/logs/api/v1"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/controllers/remote"
	"sigs.k8s.io/cluster-api/feature"
	"sigs.k8s.io/cluster-api/util/flags"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	infrav1b1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta1"
	infrav1b2 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta2"
	infrav1b3 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/internal/controllers"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/scope"
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
	profilerAddress             string
	enableContentionProfiling   bool
	syncPeriod                  time.Duration
	restConfigQPS               float32
	restConfigBurst             int
	webhookCertDir              string
	webhookCertName             string
	webhookKeyName              string
	webhookPort                 int
	healthAddr                  string
	managerOptions              = flags.ManagerOptions{}
	logOptions                  = logs.NewOptions()
	showVersion                 bool

	cloudStackClusterConcurrency         int
	cloudStackMachineConcurrency         int
	cloudStackMachineTemplateConcurrency int
	cloudStackAffinityGroupConcurrency   int
	cloudStackFailureDomainConcurrency   int
	cloudStackIsolatedNetworkConcurrency int
)

func initFlags(fs *pflag.FlagSet) {
	logsv1.AddFlags(logOptions, fs)

	fs.BoolVar(
		&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")

	fs.DurationVar(&leaderElectionLeaseDuration, "leader-elect-lease-duration", 15*time.Second,
		"Interval at which non-leader candidates will wait to force acquire leadership (duration string)")

	fs.DurationVar(&leaderElectionRenewDeadline, "leader-elect-renew-deadline", 10*time.Second,
		"Duration that the leading controller manager will retry refreshing leadership before giving up (duration string)")

	fs.DurationVar(&leaderElectionRetryPeriod, "leader-elect-retry-period", 2*time.Second,
		"Duration the LeaderElector clients should wait between tries of actions (duration string)")

	fs.StringVar(&watchNamespace, "namespace", "",
		"Namespace that the controller watches to reconcile cluster-api objects. If unspecified, the controller watches for cluster-api objects across all namespaces.")

	fs.StringVar(&watchFilterValue, "watch-filter", "",
		fmt.Sprintf("Label value that the controller watches to reconcile cluster-api objects. Label key is always %s. If unspecified, the controller watches for all cluster-api objects.", clusterv1.WatchLabel))

	fs.StringVar(&leaderElectionNamespace, "leader-elect-namespace", "",
		"Namespace that the controller performs leader election in. If unspecified, the controller will discover which namespace it is running in.",
	)

	fs.StringVar(&profilerAddress, "profiler-address", "",
		"Bind address to expose the pprof profiler (e.g. localhost:6060)")

	fs.BoolVar(&enableContentionProfiling, "contention-profiling", false,
		"Enable block profiling")

	fs.IntVar(&cloudStackClusterConcurrency, "cloudstackcluster-concurrency", 10,
		"Maximum concurrent reconciles for CloudStackCluster resources",
	)

	fs.IntVar(&cloudStackMachineConcurrency, "cloudstackmachine-concurrency", 10,
		"Maximum concurrent reconciles for CloudStackMachine resources",
	)

	fs.IntVar(&cloudStackMachineTemplateConcurrency, "cloudstackmachinetemplate-concurrency", 5,
		"Maximum concurrent reconciles for CloudStackMachineTemplate resources",
	)

	fs.IntVar(&cloudStackAffinityGroupConcurrency, "cloudstackaffinitygroup-concurrency", 5,
		"Maximum concurrent reconciles for CloudStackAffinityGroup resources",
	)

	fs.IntVar(&cloudStackFailureDomainConcurrency, "cloudstackfailuredomain-concurrency", 5,
		"Maximum concurrent reconciles for CloudStackFailureDomain resources",
	)

	fs.IntVar(&cloudStackIsolatedNetworkConcurrency, "cloudstackisolatednetwork-concurrency", 5,
		"Maximum concurrent reconciles for CloudStackIsolatedNetwork resources",
	)

	fs.DurationVar(&syncPeriod, "sync-period", 10*time.Minute,
		"The minimum interval at which watched resources are reconciled (e.g. 15m)",
	)

	fs.Float32Var(&restConfigQPS, "kube-api-qps", 20,
		"Maximum queries per second from the controller client to the Kubernetes API server.")

	fs.IntVar(&restConfigBurst, "kube-api-burst", 30,
		"Maximum number of queries that should be allowed in one burst from the controller client to the Kubernetes API server.")

	fs.IntVar(&webhookPort, "webhook-port", 9443,
		"Webhook server port")

	fs.StringVar(&webhookCertDir, "webhook-cert-dir", "/tmp/k8s-webhook-server/serving-certs/",
		"Webhook cert dir.")

	fs.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt",
		"Webhook cert name.")

	fs.StringVar(&webhookKeyName, "webhook-key-name", "tls.key",
		"Webhook key name.")

	fs.StringVar(&healthAddr, "health-addr", ":9440",
		"The address the health endpoint binds to.")

	fs.BoolVar(&showVersion, "version", false, "Show current version and exit.")

	flags.AddManagerOptions(fs, &managerOptions)

	feature.MutableGates.AddFlag(fs)
}

// Add RBAC for the authorized diagnostics endpoint.
// +kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create

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

	restConfig := ctrl.GetConfigOrDie()
	restConfig.QPS = restConfigQPS
	restConfig.Burst = restConfigBurst
	restConfig.UserAgent = remote.DefaultClusterAPIUserAgent("cluster-api-provider-cloudstack")

	if profilerAddress != "" {
		klog.Infof("Profiler listening for requests at %s", profilerAddress)
		go func() {
			klog.Info(http.ListenAndServe(profilerAddress, nil)) //nolint:gosec
		}()
	}

	tlsOptions, metricsOptions, err := flags.GetManagerOptions(managerOptions)
	if err != nil {
		setupLog.Error(err, "Unable to start manager: invalid flags")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	var watchNamespaces map[string]cache.Config
	if watchNamespace != "" {
		setupLog.Info("Watching cluster-api objects only in namespace for reconciliation", "namespace", watchNamespace)
		watchNamespaces = map[string]cache.Config{
			watchNamespace: {},
		}
	}

	if enableContentionProfiling {
		goruntime.SetBlockProfileRate(1)
	}

	// Create the controller manager.
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                     scheme,
		LeaderElection:             enableLeaderElection,
		LeaderElectionID:           "capc-leader-election-controller",
		LeaderElectionNamespace:    leaderElectionNamespace,
		LeaseDuration:              &leaderElectionLeaseDuration,
		RenewDeadline:              &leaderElectionRenewDeadline,
		RetryPeriod:                &leaderElectionRetryPeriod,
		LeaderElectionResourceLock: resourcelock.LeasesResourceLock,
		HealthProbeBindAddress:     healthAddr,
		PprofBindAddress:           profilerAddress,
		Metrics:                    *metricsOptions,
		Cache: cache.Options{
			DefaultNamespaces: watchNamespaces,
			SyncPeriod:        &syncPeriod,
		},
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{
					&corev1.ConfigMap{},
					&corev1.Secret{},
				},
			},
		},
		WebhookServer: webhook.NewServer(
			webhook.Options{
				Port:     webhookPort,
				CertDir:  webhookCertDir,
				CertName: webhookCertName,
				KeyName:  webhookKeyName,
				TLSOpts:  tlsOptions,
			},
		),
	})
	if err != nil {
		setupLog.Error(err, "Unable to start manager")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	// Setup the context that's going to be used in controllers and for the manager.
	ctx := ctrl.SetupSignalHandler()

	setupChecks(mgr)
	setupReconcilers(ctx, mgr)
	setupWebhooks(mgr)

	// +kubebuilder:scaffold:builder
	setupLog.Info("starting manager", "version", version.Get().String())
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "Problem running manager")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
}

func setupChecks(mgr ctrl.Manager) {
	if err := mgr.AddReadyzCheck("webhook", mgr.GetWebhookServer().StartedChecker()); err != nil {
		setupLog.Error(err, "Unable to create ready check")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("webhook", mgr.GetWebhookServer().StartedChecker()); err != nil {
		setupLog.Error(err, "Unable to create health check")
		os.Exit(1)
	}
}

func setupReconcilers(ctx context.Context, mgr manager.Manager) {
	scopeFactory := scope.NewClientScopeFactory(10)
	if err := (&controllers.CloudStackClusterReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		Recorder:         mgr.GetEventRecorderFor("cloudstackcluster-controller"),
		WatchFilterValue: watchFilterValue,
	}).SetupWithManager(ctx, mgr, controller.Options{MaxConcurrentReconciles: cloudStackClusterConcurrency}); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CloudStackCluster")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	if err := (&controllers.CloudStackMachineReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		Recorder:         mgr.GetEventRecorderFor("cloudstackmachine-controller"),
		WatchFilterValue: watchFilterValue,
		ScopeFactory:     scopeFactory,
	}).SetupWithManager(ctx, mgr, controller.Options{MaxConcurrentReconciles: cloudStackMachineConcurrency}); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CloudStackMachine")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	if err := (&controllers.CloudStackMachineTemplateReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		WatchFilterValue: watchFilterValue,
		ScopeFactory:     scopeFactory,
	}).SetupWithManager(ctx, mgr, controller.Options{MaxConcurrentReconciles: cloudStackMachineTemplateConcurrency}); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CloudStackMachineTemplate")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	if err := (&controllers.CloudStackIsolatedNetworkReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		Recorder:         mgr.GetEventRecorderFor("cloudstackisolatednetwork-controller"),
		WatchFilterValue: watchFilterValue,
		ScopeFactory:     scopeFactory,
	}).SetupWithManager(ctx, mgr, controller.Options{MaxConcurrentReconciles: cloudStackIsolatedNetworkConcurrency}); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CloudStackIsolatedNetwork")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	if err := (&controllers.CloudStackAffinityGroupReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		Recorder:         mgr.GetEventRecorderFor("cloudstackaffinitygroup-controller"),
		WatchFilterValue: watchFilterValue,
		ScopeFactory:     scopeFactory,
	}).SetupWithManager(ctx, mgr, controller.Options{MaxConcurrentReconciles: cloudStackAffinityGroupConcurrency}); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CloudStackAffinityGroup")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	if err := (&controllers.CloudStackFailureDomainReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		Recorder:         mgr.GetEventRecorderFor("cloudstackfailuredomain-controller"),
		WatchFilterValue: watchFilterValue,
		ScopeFactory:     scopeFactory,
	}).SetupWithManager(ctx, mgr, controller.Options{MaxConcurrentReconciles: cloudStackFailureDomainConcurrency}); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CloudStackFailureDomain")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
}

func setupWebhooks(mgr ctrl.Manager) {
	if err := (&infrav1b3.CloudStackClusterWebhook{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "CloudStackCluster")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	if err := (&infrav1b3.CloudStackClusterTemplateWebhook{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "CloudStackClusterTemplate")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	if err := (&infrav1b3.CloudStackMachineWebhook{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "CloudStackMachine")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
	if err := (&infrav1b3.CloudStackMachineTemplateWebhook{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "CloudStackMachineTemplate")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
}
