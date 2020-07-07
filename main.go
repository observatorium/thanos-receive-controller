package main

import (
	"context"
	"crypto/md5" //nolint:gosec
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	stdlog "log"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/run"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/thanos-io/thanos/pkg/receive"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
)

const (
	resyncPeriod                  = 5 * time.Minute
	pollPeriod                    = 1 * time.Second
	pollTimeout                   = 5 * time.Minute
	internalServerShutdownTimeout = time.Second
	hashringLabelKey              = "controller.receive.thanos.io/hashring"
	objstoreSecretLabelKey        = "controller.receive.thanos.io/objstore-secret"
	objstoreSecretKeyLabelKey     = "controller.receive.thanos.io/objstore-secret-key"
	envVarSecretLabelKey          = "controller.receive.thanos.io/env-var-secret"
	fromMountPath                 = "/pvc/from"

	// Metric label values
	fetchLabel  = "fetch"
	decodeLabel = "decode"
	saveLabel   = "save"
	createLabel = "create"
	deleteLabel = "delete"
	updateLabel = "update"
	otherLabel  = "other"
)

func main() {
	config := struct {
		KubeConfig             string
		Namespace              string
		StatefulSetLabel       string
		ClusterDomain          string
		ConfigMapName          string
		ConfigMapGeneratedName string
		FileName               string
		Port                   int
		Scheme                 string
		InternalAddr           string
		CleanupImage           string
		CleanUp                bool
	}{}

	flag.StringVar(&config.KubeConfig, "kubeconfig", "", "Path to kubeconfig")
	flag.StringVar(&config.Namespace, "namespace", "default", "The namespace to watch")
	flag.StringVar(&config.StatefulSetLabel, "statefulset-label", "controller.receive.thanos.io=thanos-receive-controller", "The label StatefulSets must have to be watched by the controller")
	flag.StringVar(&config.ClusterDomain, "cluster-domain", "cluster.local", "The DNS domain of the cluster")
	flag.StringVar(&config.ConfigMapName, "configmap-name", "", "The name of the original ConfigMap containing the hashring tenant configuration")
	flag.StringVar(&config.ConfigMapGeneratedName, "configmap-generated-name", "", "The name of the generated and populated ConfigMap")
	flag.StringVar(&config.FileName, "file-name", "", "The name of the configuration file in the ConfigMap")
	flag.IntVar(&config.Port, "port", 10901, "The port on which receive components are listening for write requests")
	flag.StringVar(&config.Scheme, "scheme", "http", "The URL scheme on which receive components accept write requests")
	flag.StringVar(&config.InternalAddr, "internal-addr", ":8080", "The address on which internal server runs")
	flag.StringVar(&config.CleanupImage, "cleanup-image", "quay.io/observatorium/thanos-replicate", "The container image to use for cleanup operations")
	flag.BoolVar(&config.CleanUp, "cleanup", true, "Should the controller clean up PVCs?")
	flag.Parse()

	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	logger = log.WithPrefix(logger, "ts", log.DefaultTimestampUTC)
	logger = log.WithPrefix(logger, "caller", log.DefaultCaller)

	parts := strings.Split(config.StatefulSetLabel, "=")
	if len(parts) != 2 { //nolint,gonmd
		stdlog.Fatal("--statefulset-label must be of the form 'key=value'")
	}

	labelKey, labelValue := parts[0], parts[1]

	konfig, err := clientcmd.BuildConfigFromFlags("", config.KubeConfig)
	if err != nil {
		stdlog.Fatal(err)
	}

	klient, err := kubernetes.NewForConfig(konfig)
	if err != nil {
		stdlog.Fatal(err)
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	)

	cache.SetReflectorMetricsProvider(newReflectorMetrics(reg))

	var g run.Group
	{
		// Signal channels must be buffered.
		sig := make(chan os.Signal, 1)
		g.Add(func() error {
			signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
			<-sig
			return nil
		}, func(_ error) {
			level.Info(logger).Log("msg", "caught interrupt")
			close(sig)
		})
	}
	{
		opt := &options{
			clusterDomain:          config.ClusterDomain,
			configMapName:          config.ConfigMapName,
			configMapGeneratedName: config.ConfigMapGeneratedName,
			fileName:               config.FileName,
			namespace:              config.Namespace,
			port:                   config.Port,
			scheme:                 config.Scheme,
			labelKey:               labelKey,
			labelValue:             labelValue,
			cleanupImage:           config.CleanupImage,
			cleanUp:                config.CleanUp,
		}
		c := newController(klient, logger, opt)
		c.registerMetrics(reg)
		done := make(chan struct{})

		g.Add(func() error {
			return c.run(done)
		}, func(_ error) {
			level.Info(logger).Log("msg", "shutting down controller")
			close(done)
		})
	}
	{
		router := http.NewServeMux()
		router.Handle("/metrics", promhttp.InstrumentMetricHandler(reg, promhttp.HandlerFor(reg, promhttp.HandlerOpts{})))
		router.HandleFunc("/debug/pprof/", pprof.Index)
		router.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		router.HandleFunc("/debug/pprof/profile", pprof.Profile)
		router.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		router.HandleFunc("/debug/pprof/trace", pprof.Trace)

		srv := &http.Server{Addr: config.InternalAddr, Handler: router}

		g.Add(srv.ListenAndServe, func(err error) {
			if err == http.ErrServerClosed {
				level.Warn(logger).Log("msg", "internal server closed unexpectedly")
				return
			}
			level.Info(logger).Log("msg", "shutting down internal server")
			ctx, cancel := context.WithTimeout(context.Background(), internalServerShutdownTimeout)
			if err := srv.Shutdown(ctx); err != nil {
				cancel()
				stdlog.Fatal(err)
			}
			cancel()
		})
	}

	level.Info(logger).Log("msg", "starting the controller")

	if err := g.Run(); err != nil {
		stdlog.Fatal(err)
	}
}

type prometheusReflectorMetrics struct {
	listsMetric               prometheus.Counter
	listDurationMetric        prometheus.Summary
	itemsInListMetric         prometheus.Summary
	watchesMetric             prometheus.Counter
	shortWatchesMetric        prometheus.Counter
	watchDurationMetric       prometheus.Summary
	itemsInWatchMetric        prometheus.Summary
	lastResourceVersionMetric prometheus.Gauge
}

func newReflectorMetrics(reg *prometheus.Registry) prometheusReflectorMetrics {
	m := prometheusReflectorMetrics{
		listsMetric: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "thanos_receive_controller_client_cache_lists_total",
				Help: "Total number of list operations.",
			},
		),
		listDurationMetric: prometheus.NewSummary(
			prometheus.SummaryOpts{
				Name:       "thanos_receive_controller_client_cache_list_duration_seconds",
				Help:       "Duration of a Kubernetes API call in seconds.",
				Objectives: map[float64]float64{},
			},
		),
		itemsInListMetric: prometheus.NewSummary(
			prometheus.SummaryOpts{
				Name:       "thanos_receive_controller_client_cache_list_items",
				Help:       "Count of items in a list from the Kubernetes API.",
				Objectives: map[float64]float64{},
			},
		),
		watchesMetric: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "thanos_receive_controller_client_cache_watches_total",
				Help: "Total number of watch operations.",
			},
		),
		shortWatchesMetric: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "thanos_receive_controller_client_cache_short_watches_total",
				Help: "Total number of short watch operations.",
			},
		),
		watchDurationMetric: prometheus.NewSummary(
			prometheus.SummaryOpts{
				Name:       "thanos_receive_controller_client_cache_watch_duration_seconds",
				Help:       "Duration of watches on the Kubernetes API.",
				Objectives: map[float64]float64{},
			},
		),
		itemsInWatchMetric: prometheus.NewSummary(
			prometheus.SummaryOpts{
				Name:       "thanos_receive_controller_client_cache_watch_events",
				Help:       "Number of items in watches on the Kubernetes API.",
				Objectives: map[float64]float64{},
			},
		),
		lastResourceVersionMetric: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "thanos_receive_controller_client_cache_last_resource_version",
				Help: "Last resource version from the Kubernetes API.",
			},
		),
	}

	if reg != nil {
		reg.MustRegister(
			m.listDurationMetric,
			m.itemsInListMetric,
			m.watchesMetric,
			m.shortWatchesMetric,
			m.watchDurationMetric,
			m.itemsInWatchMetric,
			m.lastResourceVersionMetric,
		)
	}

	return m
}

func (p prometheusReflectorMetrics) NewListsMetric(name string) cache.CounterMetric {
	return p.listsMetric
}

func (p prometheusReflectorMetrics) NewListDurationMetric(name string) cache.SummaryMetric {
	return p.listDurationMetric
}

func (p prometheusReflectorMetrics) NewItemsInListMetric(name string) cache.SummaryMetric {
	return p.itemsInListMetric
}

func (p prometheusReflectorMetrics) NewWatchesMetric(name string) cache.CounterMetric {
	return p.watchesMetric
}

func (p prometheusReflectorMetrics) NewShortWatchesMetric(name string) cache.CounterMetric {
	return p.shortWatchesMetric
}

func (p prometheusReflectorMetrics) NewWatchDurationMetric(name string) cache.SummaryMetric {
	return p.watchDurationMetric
}

func (p prometheusReflectorMetrics) NewItemsInWatchMetric(name string) cache.SummaryMetric {
	return p.itemsInWatchMetric
}

func (p prometheusReflectorMetrics) NewLastResourceVersionMetric(name string) cache.GaugeMetric {
	return p.lastResourceVersionMetric
}

type options struct {
	clusterDomain          string
	configMapName          string
	configMapGeneratedName string
	fileName               string
	namespace              string
	port                   int
	scheme                 string
	labelKey               string
	labelValue             string
	cleanupImage           string
	cleanUp                bool
}

type controller struct {
	options *options
	podQ    workqueue.RateLimitingInterface
	stsQ    *queue
	logger  log.Logger

	klient  kubernetes.Interface
	cmapInf cache.SharedIndexInformer
	podInf  cache.SharedIndexInformer
	ssetInf cache.SharedIndexInformer

	reconcileAttempts                 prometheus.Counter
	reconcileErrors                   *prometheus.CounterVec
	cleanupAttempts                   prometheus.Counter
	cleanupErrors                     *prometheus.CounterVec
	configmapChangeAttempts           prometheus.Counter
	configmapChangeErrors             *prometheus.CounterVec
	configmapHash                     prometheus.Gauge
	configmapLastSuccessfulChangeTime prometheus.Gauge
	hashringNodes                     *prometheus.GaugeVec
	hashringTenants                   *prometheus.GaugeVec
}

func newController(klient kubernetes.Interface, logger log.Logger, o *options) *controller {
	if logger == nil {
		logger = log.NewNopLogger()
	}

	return &controller{
		options: o,
		podQ:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Pod"),
		stsQ:    newQueue(),
		logger:  logger,

		klient:  klient,
		cmapInf: coreinformers.NewConfigMapInformer(klient, o.namespace, resyncPeriod, nil),
		podInf:  coreinformers.NewPodInformer(klient, o.namespace, resyncPeriod, nil),
		ssetInf: appsinformers.NewFilteredStatefulSetInformer(klient, o.namespace, resyncPeriod, nil, func(lo *metav1.ListOptions) {
			lo.LabelSelector = labels.Set{o.labelKey: o.labelValue}.String()
		}),

		reconcileAttempts: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "thanos_receive_controller_reconcile_attempts_total",
			Help: "Total number of reconciles.",
		}),
		reconcileErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "thanos_receive_controller_reconcile_errors_total",
			Help: "Total number of reconciles errors.",
		},
			[]string{"type"},
		),
		cleanupAttempts: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "thanos_receive_controller_cleanup_attempts_total",
			Help: "Total number of cleanups.",
		}),
		cleanupErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "thanos_receive_controller_cleanup_errors_total",
			Help: "Total number of cleanup errors.",
		},
			[]string{"type"},
		),
		configmapChangeAttempts: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "thanos_receive_controller_configmap_change_attempts_total",
			Help: "Total number of configmap change attempts.",
		}),
		configmapChangeErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "thanos_receive_controller_configmap_change_errors_total",
			Help: "Total number of configmap change errors.",
		},
			[]string{"type"},
		),
		configmapHash: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "thanos_receive_controller_configmap_hash",
				Help: "Hash of the currently loaded configmap.",
			}),
		configmapLastSuccessfulChangeTime: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "thanos_receive_controller_configmap_last_reload_success_timestamp_seconds",
				Help: "Timestamp of the last successful configmap.",
			}),
		hashringNodes: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "thanos_receive_controller_hashring_nodes",
				Help: "The number of nodes per hashring.",
			},
			[]string{"name"},
		),
		hashringTenants: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "thanos_receive_controller_hashring_tenants",
				Help: "The number of tenants per hashring.",
			},
			[]string{"name"},
		),
	}
}

func (c *controller) registerMetrics(reg *prometheus.Registry) {
	if reg != nil {
		c.reconcileAttempts.Add(0)
		c.reconcileErrors.WithLabelValues(fetchLabel).Add(0)
		c.reconcileErrors.WithLabelValues(decodeLabel).Add(0)
		c.reconcileErrors.WithLabelValues(saveLabel).Add(0)
		c.configmapChangeAttempts.Add(0)
		c.configmapChangeErrors.WithLabelValues(createLabel).Add(0)
		c.configmapChangeErrors.WithLabelValues(updateLabel).Add(0)
		c.configmapChangeErrors.WithLabelValues(otherLabel).Add(0)
		c.cleanupErrors.WithLabelValues(createLabel).Add(0)
		c.cleanupErrors.WithLabelValues(decodeLabel).Add(0)
		c.cleanupErrors.WithLabelValues(deleteLabel).Add(0)
		c.cleanupErrors.WithLabelValues(fetchLabel).Add(0)
		reg.MustRegister(
			c.reconcileAttempts,
			c.reconcileErrors,
			c.configmapChangeAttempts,
			c.configmapChangeErrors,
			c.configmapHash,
			c.configmapLastSuccessfulChangeTime,
			c.hashringNodes,
			c.hashringTenants,
			c.cleanupErrors,
		)
	}
}

func (c *controller) run(stop <-chan struct{}) error {
	defer c.podQ.ShutDown()
	defer c.stsQ.stop()

	go c.cmapInf.Run(stop)
	go c.ssetInf.Run(stop)
	if c.options.cleanUp {
		go c.podInf.Run(stop)
	}

	if err := c.waitForCacheSync(stop); err != nil {
		return err
	}

	c.cmapInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ interface{}) { c.stsQ.add() },
		DeleteFunc: func(_ interface{}) { c.stsQ.add() },
		UpdateFunc: func(_, _ interface{}) { c.stsQ.add() },
	})
	c.ssetInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ interface{}) { c.stsQ.add() },
		DeleteFunc: func(_ interface{}) { c.stsQ.add() },
		UpdateFunc: func(_, _ interface{}) { c.stsQ.add() },
	})

	go c.stsWorker()

	if c.options.cleanUp {
		c.podInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
			DeleteFunc: func(obj interface{}) { c.podQ.Add(obj) },
		})
		go c.podWorker()
	}

	<-stop

	return nil
}

// waitForCacheSync waits for the informers' caches to be synced.
func (c *controller) waitForCacheSync(stop <-chan struct{}) error {
	ok := true
	informers := []struct {
		name     string
		informer cache.SharedIndexInformer
	}{
		{"ConfigMap", c.cmapInf},
		{"StatefulSet", c.ssetInf},
		{"Pod", c.podInf},
	}
	if !c.options.cleanUp {
		informers = informers[:1]
	}

	for _, inf := range informers {
		if !cache.WaitForCacheSync(stop, inf.informer.HasSynced) {
			level.Error(c.logger).Log("msg", fmt.Sprintf("failed to sync %s cache", inf.name))

			ok = false
		} else {
			level.Debug(c.logger).Log("msg", fmt.Sprintf("successfully synced %s cache", inf.name))
		}
	}

	if !ok {
		return errors.New("failed to sync caches")
	}

	level.Info(c.logger).Log("msg", "successfully synced all caches")

	return nil
}

func (c *controller) podWorker() {
	fn := func() bool {
		key, quit := c.podQ.Get()
		if quit {
			return false
		}
		defer c.podQ.Done(key)
		if err := c.cleanUp(key.(*corev1.Pod)); err != nil {
			level.Error(c.logger).Log("msg", "unable to clean up PVC", "err", err)
			c.podQ.AddRateLimited(key)
			return true
		}
		c.podQ.Forget(key)
		return true
	}
	for fn() {
	}
}

func (c *controller) stsWorker() {
	for c.stsQ.get() {
		c.sync()
	}
}

func (c *controller) resolvePodOwnerRef(namespace string, refs []metav1.OwnerReference) (*appsv1.StatefulSet, error) {
	for _, ref := range refs {
		// If the owner reference points at the wrong kind of object, skip.
		if ref.Kind != "StatefulSet" {
			continue
		}
		// If the owner reference points at something that we don't have, then skip.
		obj, ok, err := c.ssetInf.GetStore().GetByKey(fmt.Sprintf("%s/%s", namespace, ref.Name))
		if !ok {
			continue
		}
		if err != nil {
			return nil, err
		}
		sts := obj.(*appsv1.StatefulSet)
		if sts.UID != ref.UID {
			return nil, errors.Wrap(err, "owner reference UID does not match StatefulSet")
		}
		return sts, nil
	}
	return nil, nil
}

func (c *controller) generateHelper(name string, sts *appsv1.StatefulSet) *batchv1.Job {
	initContainerTemplate := corev1.Container{
		Name:            "replicate",
		Image:           c.options.cleanupImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Args: []string{
			"run",
			"--single-run",
			"--objstorefrom.config=$(OBJSTORE_CONFIG_FROM)",
			"--objstoreto.config=$(OBJSTORE_CONFIG_TO)",
			"--log.level=debug",
		},
		Env: []corev1.EnvVar{
			{
				Name:  "OBJSTORE_CONFIG_FROM",
				Value: "type: FILESYSTEM\nconfig:\n  directory: " + fromMountPath,
			},
			{
				Name: "OBJSTORE_CONFIG_TO",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: sts.Labels[objstoreSecretLabelKey],
						},
						Key: sts.Labels[objstoreSecretKeyLabelKey],
					},
				},
			},
		},
	}
	// Inject extra environment variables into the cleanup Pod if provided.
	if _, ok := sts.Labels[envVarSecretLabelKey]; ok {
		initContainerTemplate.EnvFrom = []corev1.EnvFromSource{
			{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: sts.Labels[envVarSecretLabelKey],
					},
				},
			},
		}
	}

	helper := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cleanup-" + name,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:  corev1.RestartPolicyNever,
					InitContainers: make([]corev1.Container, len(sts.Spec.VolumeClaimTemplates)),
					Containers: []corev1.Container{
						{
							Name:            "cleanup",
							Image:           c.options.cleanupImage,
							Command:         []string{"rm", "-rf"},
							ImagePullPolicy: corev1.PullIfNotPresent,
							VolumeMounts:    make([]corev1.VolumeMount, len(sts.Spec.VolumeClaimTemplates)),
						},
					},
					Volumes: make([]corev1.Volume, len(sts.Spec.VolumeClaimTemplates)),
				},
			},
		},
	}
	var v corev1.Volume
	var vname, mountPath string
	for i, t := range sts.Spec.VolumeClaimTemplates {
		// Create a new Volu,e for this template.
		vname = fmt.Sprintf("%s-%s", t.Name, name)
		v = corev1.Volume{
			Name: vname,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: vname,
				},
			},
		}
		helper.Spec.Template.Spec.Volumes[i] = v
		// Create an init container to replicate this Volume.
		helper.Spec.Template.Spec.InitContainers[i] = *initContainerTemplate.DeepCopy()
		helper.Spec.Template.Spec.InitContainers[i].VolumeMounts = []corev1.VolumeMount{{
			Name:      v.Name,
			MountPath: fromMountPath,
		}}
		// Add this Volume to the container that removes files.
		mountPath = filepath.Join("/pvc", vname)
		helper.Spec.Template.Spec.Containers[0].VolumeMounts[i] = corev1.VolumeMount{
			Name:      v.Name,
			MountPath: mountPath,
		}
		helper.Spec.Template.Spec.Containers[0].Command = append(helper.Spec.Template.Spec.Containers[0].Command, filepath.Join(mountPath, "*"))
	}

	return helper
}

func (c *controller) cleanUp(pod *corev1.Pod) error {
	c.cleanupAttempts.Inc()
	sts, err := c.resolvePodOwnerRef(pod.Namespace, pod.GetOwnerReferences())
	if err != nil {
		c.cleanupErrors.WithLabelValues(fetchLabel).Inc()
		return errors.Wrap(err, "could not get StatefulSet")
	}
	// This probably means that the Pod did not belong to a StatefulSet with
	// our label selector, i.e. not a StatefulSet we are watching.
	if sts == nil {
		return nil
	}
	// Nothing to clean up.
	if len(sts.Spec.VolumeClaimTemplates) == 0 {
		return nil
	}

	_, secretOK := sts.Labels[objstoreSecretLabelKey]
	_, keyOK := sts.Labels[objstoreSecretKeyLabelKey]
	if !secretOK || !keyOK {
		c.cleanupErrors.WithLabelValues(decodeLabel).Inc()
		return fmt.Errorf("StatefulSet %s/%s is missing either the %s or %s label", sts.Namespace, sts.Name, objstoreSecretLabelKey, objstoreSecretKeyLabelKey)
	}
	helper := c.generateHelper(pod.Name, sts)

	if _, err := c.klient.BatchV1().Jobs(pod.Namespace).Create(helper); err != nil && !kerrors.IsAlreadyExists(err) {
		c.cleanupErrors.WithLabelValues(createLabel).Inc()
		return errors.Wrap(err, "unable to create the cleanup Pod")
	}

	defer func() {
		policy := metav1.DeletePropagationForeground
		err := c.klient.BatchV1().Jobs(pod.Namespace).Delete(helper.Name, &metav1.DeleteOptions{PropagationPolicy: &policy})
		if err != nil {
			level.Error(c.logger).Log("msg", "unable to delete the cleanup Job", "err", err)
			c.cleanupErrors.WithLabelValues(deleteLabel).Inc()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), pollTimeout)
	defer cancel()

	err = wait.PollUntil(pollPeriod,
		func() (bool, error) {
			if j, err := c.klient.BatchV1().Jobs(pod.Namespace).Get(helper.Name, v1.GetOptions{}); err != nil {
				return false, err
			} else if j.Status.Succeeded == 1 {
				return true, nil
			}
			return false, nil
		},
		ctx.Done())
	if err != nil {
		c.cleanupErrors.WithLabelValues(fetchLabel).Inc()
		return errors.Wrap(err, "clean up PersistentVolumeClaim")
	}

	return nil
}

func (c *controller) sync() {
	c.reconcileAttempts.Inc()
	configMap, ok, err := c.cmapInf.GetStore().GetByKey(fmt.Sprintf("%s/%s", c.options.namespace, c.options.configMapName))

	if !ok || err != nil {
		c.reconcileErrors.WithLabelValues(fetchLabel).Inc()
		level.Warn(c.logger).Log("msg", "could not fetch ConfigMap", "err", err, "name", c.options.configMapName)

		return
	}

	cm := configMap.(*corev1.ConfigMap)

	var hashrings []receive.HashringConfig
	if err := json.Unmarshal([]byte(cm.Data[c.options.fileName]), &hashrings); err != nil {
		c.reconcileErrors.WithLabelValues(decodeLabel).Inc()
		level.Warn(c.logger).Log("msg", "failed to decode configuration", "err", err)

		return
	}

	statefulsets := make(map[string]*appsv1.StatefulSet)

	for _, obj := range c.ssetInf.GetStore().List() {
		sts := obj.(*appsv1.StatefulSet)
		hashring, ok := sts.Labels[hashringLabelKey]

		if !ok {
			continue
		}

		statefulsets[hashring] = sts.DeepCopy()
	}

	c.populate(hashrings, statefulsets)

	if err := c.saveHashring(hashrings); err != nil {
		c.reconcileErrors.WithLabelValues(saveLabel).Inc()
		level.Error(c.logger).Log("msg", "failed to save hashrings")
	}
}

func (c *controller) populate(hashrings []receive.HashringConfig, statefulsets map[string]*appsv1.StatefulSet) {
	for i, h := range hashrings {
		if sts, exists := statefulsets[h.Hashring]; exists {
			var endpoints []string
			for i := 0; i < int(*sts.Spec.Replicas); i++ {
				endpoints = append(endpoints,
					fmt.Sprintf("%s-%d.%s.%s.svc.%s:%d",
						sts.Name,
						i,
						sts.Spec.ServiceName,
						c.options.namespace,
						c.options.clusterDomain,
						c.options.port,
					),
				)
			}

			hashrings[i].Endpoints = endpoints
			c.hashringNodes.WithLabelValues(h.Hashring).Set(float64(len(endpoints)))
			c.hashringTenants.WithLabelValues(h.Hashring).Set(float64(len(h.Tenants)))
		}
	}
}

func (c *controller) saveHashring(hashring []receive.HashringConfig) error {
	buf, err := json.Marshal(hashring)
	if err != nil {
		return err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.options.configMapGeneratedName,
			Namespace: c.options.namespace,
		},
		Data: map[string]string{
			c.options.fileName: string(buf),
		},
		BinaryData: nil,
	}

	c.configmapHash.Set(hashAsMetricValue(buf))
	c.configmapChangeAttempts.Inc()
	gcm, err := c.klient.CoreV1().ConfigMaps(c.options.namespace).Get(c.options.configMapGeneratedName, metav1.GetOptions{})

	if kerrors.IsNotFound(err) {
		_, err = c.klient.CoreV1().ConfigMaps(c.options.namespace).Create(cm)
		if err != nil {
			c.configmapChangeErrors.WithLabelValues(createLabel).Inc()
			return err
		}

		c.configmapLastSuccessfulChangeTime.Set(float64(time.Now().Unix()))

		return nil
	}

	if err != nil {
		c.configmapChangeErrors.WithLabelValues(otherLabel).Inc()
		return err
	}

	if gcm.Data[c.options.fileName] == cm.Data[c.options.fileName] {
		return nil
	}

	_, err = c.klient.CoreV1().ConfigMaps(c.options.namespace).Update(cm)
	if err != nil {
		c.configmapChangeErrors.WithLabelValues(updateLabel).Inc()
		return err
	}

	c.configmapLastSuccessfulChangeTime.Set(float64(time.Now().Unix()))

	return nil
}

// hashAsMetricValue generates metric value from hash of data.
func hashAsMetricValue(data []byte) float64 {
	sum := md5.Sum(data) //nolint:gosec
	// We only want 48 bits as a float64 only has a 53 bit mantissa.
	smallSum := sum[0:6]

	var bytes = make([]byte, 8)

	copy(bytes, smallSum)

	return float64(binary.LittleEndian.Uint64(bytes))
}

// queue is a non-blocking queue.
type queue struct {
	sync.Mutex
	ch chan struct{}
	ok bool
}

func newQueue() *queue {
	// We want a buffer of size 1 to queue updates
	// while a dequeuer is busy.
	return &queue{ch: make(chan struct{}, 1), ok: true}
}

func (q *queue) add() {
	q.Lock()
	defer q.Unlock()

	if !q.ok {
		return
	}
	select {
	case q.ch <- struct{}{}:
	default:
	}
}

func (q *queue) stop() {
	q.Lock()
	defer q.Unlock()

	if !q.ok {
		return
	}

	close(q.ch)
	q.ok = false
}

func (q *queue) get() bool {
	<-q.ch
	q.Lock()
	defer q.Unlock()

	return q.ok
}
