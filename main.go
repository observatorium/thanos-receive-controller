package main

import (
	"context"
	"crypto/md5" //nolint:gosec
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	stdlog "log"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/thanos-io/thanos/pkg/receive"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubectl/pkg/util/podutils"
)

type label = string

const (
	defaultPort = 10901

	resyncPeriod                  = 5 * time.Minute
	defaultScaleTimeout           = 5 * time.Second
	internalServerShutdownTimeout = time.Second
	hashringLabelKey              = "controller.receive.thanos.io/hashring"

	// Metric label values
	fetch  label = "fetch"
	decode label = "decode"
	save   label = "save"
	poll   label = "poll"
	create label = "create"
	update label = "update"
	other  label = "other"
)

type CmdConfig struct {
	KubeConfig             string
	Namespace              string
	StatefulSetLabel       string
	Label                  string
	ClusterDomain          string
	ConfigMapName          string
	ConfigMapGeneratedName string
	FileName               string
	Port                   int
	Scheme                 string
	InternalAddr           string
	AllowOnlyReadyReplicas bool
	AllowDynamicScaling    bool
	AnnotatePodsOnChange   bool
	AnnotatePodsLabel      string
	ScaleTimeout           time.Duration
}

func parseFlags() CmdConfig {
	var config CmdConfig

	flag.StringVar(&config.KubeConfig, "kubeconfig", "", "Path to kubeconfig")
	flag.StringVar(&config.Namespace, "namespace", "default", "The namespace to watch")
	flag.StringVar(&config.StatefulSetLabel, "statefulset-label", "", "[DEPRECATED] The label StatefulSets must have to be watched by the controller")
	flag.StringVar(&config.Label, "label", "controller.receive.thanos.io=thanos-receive-controller", "The label workloads must have to be watched by the controller.")
	flag.StringVar(&config.ClusterDomain, "cluster-domain", "cluster.local", "The DNS domain of the cluster")
	flag.StringVar(&config.ConfigMapName, "configmap-name", "", "The name of the original ConfigMap containing the hashring tenant configuration")
	flag.StringVar(&config.ConfigMapGeneratedName, "configmap-generated-name", "", "The name of the generated and populated ConfigMap")
	flag.StringVar(&config.FileName, "file-name", "", "The name of the configuration file in the ConfigMap")
	flag.IntVar(&config.Port, "port", defaultPort, "The port on which receive components are listening for write requests")
	flag.StringVar(&config.Scheme, "scheme", "http", "The URL scheme on which receive components accept write requests")
	flag.StringVar(&config.InternalAddr, "internal-addr", ":8080", "The address on which internal server runs")
	flag.BoolVar(&config.AllowOnlyReadyReplicas, "allow-only-ready-replicas", false, "Populate only Ready receiver replicas in the hashring configuration")
	flag.BoolVar(&config.AllowDynamicScaling, "allow-dynamic-scaling", false, "Update the hashring configuration on scale down events.")
	flag.BoolVar(&config.AnnotatePodsOnChange, "annotate-pods-on-change", false, "Annotates pods with current timestamp on a hashring change")
	flag.StringVar(&config.AnnotatePodsLabel, "annotate-pods-label", "", "The label pods must have to be annotated with current timestamp by the controller on a hashring change.")
	flag.DurationVar(&config.ScaleTimeout, "scale-timeout", defaultScaleTimeout, "A timeout to wait for receivers to really start after they report healthy")
	flag.Parse()

	return config
}

func main() {
	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	logger = log.WithPrefix(logger, "ts", log.DefaultTimestampUTC)
	logger = log.WithPrefix(logger, "caller", log.DefaultCaller)

	config := parseFlags()

	var tmpControllerLabel string
	if len(config.StatefulSetLabel) > 0 {
		tmpControllerLabel = config.StatefulSetLabel

		level.Warn(logger).Log("msg", "The --statefulset-label flag is deprecated. Please see the manual page for updates.")
	} else {
		tmpControllerLabel = config.Label
	}

	labelKey, labelValue := splitLabel(tmpControllerLabel)

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
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	cache.SetReflectorMetricsProvider(newReflectorMetrics(reg))

	var g run.Group
	{
		g.Add(run.SignalHandler(context.Background(), os.Interrupt, syscall.SIGTERM))
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
			allowOnlyReadyReplicas: config.AllowOnlyReadyReplicas,
			annotatePodsOnChange:   config.AnnotatePodsOnChange,
			annotatePodsLabel:      config.AnnotatePodsLabel,
			allowDynamicScaling:    config.AllowDynamicScaling,
			scaleTimeout:           config.ScaleTimeout,
		}
		c := newController(klient, logger, opt)
		c.registerMetrics(reg)
		done := make(chan struct{})

		g.Add(func() error {
			return c.run(context.TODO(), done)
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
			if errors.Is(err, http.ErrServerClosed) {
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

const labelParts = 2

func splitLabel(in string) (key, value string) {
	parts := strings.Split(in, "=")
	if len(parts) != labelParts {
		stdlog.Fatal("Labels consist of a key-value pair f.ex: 'key=value'")
	}

	return parts[0], parts[1]
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
	allowOnlyReadyReplicas bool
	allowDynamicScaling    bool
	annotatePodsOnChange   bool
	annotatePodsLabel      string
	scaleTimeout           time.Duration
}

type controller struct {
	options *options
	queue   *queue
	logger  log.Logger

	// should be fine without a mutex, as sync only ever runs once at a time.
	replicas map[string]int32

	klient  kubernetes.Interface
	cmapInf cache.SharedIndexInformer
	ssetInf cache.SharedIndexInformer

	reconcileAttempts                 prometheus.Counter
	reconcileErrors                   *prometheus.CounterVec
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
		queue:   newQueue(),
		logger:  logger,

		replicas: make(map[string]int32),

		klient:  klient,
		cmapInf: coreinformers.NewConfigMapInformer(klient, o.namespace, resyncPeriod, nil),
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
		c.reconcileErrors.WithLabelValues(fetch).Add(0)
		c.reconcileErrors.WithLabelValues(decode).Add(0)
		c.reconcileErrors.WithLabelValues(save).Add(0)
		c.reconcileErrors.WithLabelValues(poll).Add(0)
		c.configmapChangeAttempts.Add(0)
		c.configmapChangeErrors.WithLabelValues(create).Add(0)
		c.configmapChangeErrors.WithLabelValues(update).Add(0)
		c.configmapChangeErrors.WithLabelValues(other).Add(0)
		reg.MustRegister(
			c.reconcileAttempts,
			c.reconcileErrors,
			c.configmapChangeAttempts,
			c.configmapChangeErrors,
			c.configmapHash,
			c.configmapLastSuccessfulChangeTime,
			c.hashringNodes,
			c.hashringTenants,
		)
	}
}

func (c *controller) run(ctx context.Context, stop <-chan struct{}) error {
	defer c.queue.stop()

	go c.cmapInf.Run(stop)
	go c.ssetInf.Run(stop)

	if err := c.waitForCacheSync(stop); err != nil {
		return err
	}

	_, err := c.cmapInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ interface{}) { c.queue.add() },
		DeleteFunc: func(_ interface{}) { c.queue.add() },
		UpdateFunc: func(_, _ interface{}) { c.queue.add() },
	})
	if err != nil {
		return err
	}

	_, err = c.ssetInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ interface{}) { c.queue.add() },
		DeleteFunc: func(_ interface{}) { c.queue.add() },
		UpdateFunc: func(_, _ interface{}) { c.queue.add() },
	})

	if err != nil {
		return err
	}

	go c.worker(ctx)

	<-stop

	return nil
}

var errCacheSync = errors.New("failed to sync caches")

// waitForCacheSync waits for the informers' caches to be synced.
func (c *controller) waitForCacheSync(stop <-chan struct{}) error {
	ok := true
	informers := []struct {
		name     string
		informer cache.SharedIndexInformer
	}{
		{"ConfigMap", c.cmapInf},
		{"StatefulSet", c.ssetInf},
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
		return errCacheSync
	}

	level.Info(c.logger).Log("msg", "successfully synced all caches")

	return nil
}

func (c *controller) worker(ctx context.Context) {
	for c.queue.get() {
		c.sync(ctx)
	}
}

func (c *controller) sync(ctx context.Context) {
	c.reconcileAttempts.Inc()
	configMap, ok, err := c.cmapInf.GetStore().GetByKey(fmt.Sprintf("%s/%s", c.options.namespace, c.options.configMapName))

	if !ok || err != nil {
		c.reconcileErrors.WithLabelValues(fetch).Inc()
		level.Warn(c.logger).Log("msg", "could not fetch ConfigMap", "err", err, "name", c.options.configMapName)

		return
	}

	cm, ok := configMap.(*corev1.ConfigMap)
	if !ok {
		level.Error(c.logger).Log("msg", "failed type assertion from expected ConfigMap")
	}

	var hashrings []receive.HashringConfig
	if err := json.Unmarshal([]byte(cm.Data[c.options.fileName]), &hashrings); err != nil {
		c.reconcileErrors.WithLabelValues(decode).Inc()
		level.Warn(c.logger).Log("msg", "failed to decode configuration", "err", err)

		return
	}

	statefulsets := make(map[string]*appsv1.StatefulSet)

	for _, obj := range c.ssetInf.GetStore().List() {
		sts, ok := obj.(*appsv1.StatefulSet)
		if !ok {
			level.Error(c.logger).Log("msg", "failed type assertion from expected StatefulSet")
		}

		hashring, ok := sts.Labels[hashringLabelKey]
		if !ok {
			continue
		}

		// If there's an increase in replicas we poll for the new replicas to be ready
		if _, ok := c.replicas[hashring]; ok && c.replicas[hashring] < *sts.Spec.Replicas {
			// Iterate over new replicas to wait until they are running
			for i := c.replicas[hashring]; i < *sts.Spec.Replicas; i++ {
				start := time.Now()
				podName := fmt.Sprintf("%s-%d", sts.Name, i)

				if err := c.waitForPod(ctx, podName); err != nil {
					level.Warn(c.logger).Log("msg", "failed polling until pod is ready", "pod", podName, "duration", time.Since(start), "err", err)
					continue
				}

				level.Debug(c.logger).Log("msg", "waited until new pod was ready", "pod", podName, "duration", time.Since(start))
			}
		}

		c.replicas[hashring] = *sts.Spec.Replicas
		statefulsets[hashring] = sts.DeepCopy()

		time.Sleep(c.options.scaleTimeout) // Give some time for all replicas before they receive hundreds req/s
	}

	c.populate(ctx, hashrings, statefulsets)

	err = c.saveHashring(ctx, hashrings, cm)
	if err != nil {
		c.reconcileErrors.WithLabelValues(save).Inc()
		level.Error(c.logger).Log("msg", "failed to save hashrings", "err", err)
	}

	// If enabled and hashring was successfully changed, annotate pods with config hash on change.
	// This should update the configmap inside the pod instantaneously as well, as
	// opposed to having to wait kubelet sync period + cache (see
	// https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/#mounted-configmaps-are-updated-automatically)
	if err == nil && c.options.annotatePodsOnChange {
		c.annotatePods(ctx)
	}
}

func (c controller) waitForPod(ctx context.Context, name string) error {
	return wait.PollImmediate(time.Second, time.Minute, func() (bool, error) {
		pod, err := c.klient.CoreV1().Pods(c.options.namespace).Get(ctx, name, metav1.GetOptions{})
		if kerrors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		switch pod.Status.Phase {
		case corev1.PodRunning:
			if c.options.allowOnlyReadyReplicas {
				if podutils.IsPodReady(pod) {
					return true, nil
				}
				return false, nil
			}
			return true, nil
		case corev1.PodFailed, corev1.PodPending, corev1.PodSucceeded, corev1.PodUnknown:
			return false, nil
		default:
			return false, nil
		}
	})
}

func (c *controller) populate(ctx context.Context, hashrings []receive.HashringConfig, statefulsets map[string]*appsv1.StatefulSet) {
	for i, h := range hashrings {
		sts, exists := statefulsets[h.Hashring]
		if !exists {
			continue
		}

		var endpoints []string

		for i := 0; i < int(*sts.Spec.Replicas); i++ {
			if c.options.allowDynamicScaling {
				podName := fmt.Sprintf("%s-%d", sts.Name, i)

				pod, err := c.klient.CoreV1().Pods(c.options.namespace).Get(ctx, podName, metav1.GetOptions{})
				if kerrors.IsNotFound(err) {
					continue
				}
				// Do not add a replica to the hashring if pod is not Ready.
				if !podutils.IsPodReady(pod) {
					level.Warn(c.logger).Log("msg", "failed adding pod to hashring, pod not ready", "pod", podName, "err", err)
					continue
				}

				if pod.ObjectMeta.DeletionTimestamp != nil && (pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending) {
					// Pod is terminating, do not add it to the hashring.
					continue
				}
			}
			// If cluster domain is empty string we don't want dot after svc.
			clusterDomain := ""
			if c.options.clusterDomain != "" {
				clusterDomain = fmt.Sprintf(".%s", c.options.clusterDomain)
			}

			endpoints = append(endpoints,
				fmt.Sprintf("%s-%d.%s.%s.svc%s:%d",
					sts.Name,
					i,
					sts.Spec.ServiceName,
					c.options.namespace,
					clusterDomain,
					c.options.port,
				),
			)
		}

		hashrings[i].Endpoints = endpoints
		c.hashringNodes.WithLabelValues(h.Hashring).Set(float64(len(endpoints)))
		c.hashringTenants.WithLabelValues(h.Hashring).Set(float64(len(h.Tenants)))
	}
}

func (c *controller) saveHashring(ctx context.Context, hashring []receive.HashringConfig, orgCM *corev1.ConfigMap) error {
	buf, err := json.Marshal(hashring)
	if err != nil {
		return err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.options.configMapGeneratedName,
			Namespace: c.options.namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Name:       orgCM.GetName(),
					UID:        orgCM.GetUID(),
				},
			},
		},
		Data: map[string]string{
			c.options.fileName: string(buf),
		},
		BinaryData: nil,
	}

	c.configmapHash.Set(hashAsMetricValue(buf))
	c.configmapChangeAttempts.Inc()
	gcm, err := c.klient.CoreV1().ConfigMaps(c.options.namespace).Get(ctx, c.options.configMapGeneratedName, metav1.GetOptions{})

	if kerrors.IsNotFound(err) {
		_, err = c.klient.CoreV1().ConfigMaps(c.options.namespace).Create(ctx, cm, metav1.CreateOptions{})
		if err != nil {
			c.configmapChangeErrors.WithLabelValues(create).Inc()
			return err
		}

		c.configmapLastSuccessfulChangeTime.Set(float64(time.Now().Unix()))

		return nil
	}

	if err != nil {
		c.configmapChangeErrors.WithLabelValues(other).Inc()
		return err
	}

	if gcm.Data[c.options.fileName] == cm.Data[c.options.fileName] {
		return nil
	}

	_, err = c.klient.CoreV1().ConfigMaps(c.options.namespace).Update(ctx, cm, metav1.UpdateOptions{})
	if err != nil {
		c.configmapChangeErrors.WithLabelValues(update).Inc()
		return err
	}

	c.configmapLastSuccessfulChangeTime.Set(float64(time.Now().Unix()))

	return nil
}

func (c *controller) annotatePods(ctx context.Context) {
	annotationKey := fmt.Sprintf("%s/%s", c.options.labelKey, "lastControllerUpdate")
	updateTime := fmt.Sprintf("%d", time.Now().Unix())
	annotatePodsLabel := fmt.Sprintf("%s=%s", c.options.labelKey, c.options.labelValue)

	if c.options.annotatePodsLabel != "" {
		annotatePodsLabel = c.options.annotatePodsLabel
	}

	// Select pods that have a controllerLabel matching ours.
	podList, err := c.klient.CoreV1().Pods(c.options.namespace).List(ctx,
		metav1.ListOptions{
			LabelSelector: annotatePodsLabel,
		})
	if err != nil {
		level.Error(c.logger).Log("msg", "failed to list pods belonging to controller", "err", err)
		return
	}

	for _, pod := range podList.Items {
		podObj := pod.DeepCopy()

		annotations := podObj.ObjectMeta.Annotations
		if annotations == nil {
			annotations = make(map[string]string)
		}

		annotations[annotationKey] = updateTime
		podObj.SetAnnotations(annotations)

		_, err := c.klient.CoreV1().Pods(pod.Namespace).Update(ctx, podObj, metav1.UpdateOptions{})
		if err != nil {
			level.Error(c.logger).Log("msg", "failed to update pod", "err", err)
		}
	}
}

// hashAsMetricValue generates metric value from hash of data.
func hashAsMetricValue(data []byte) float64 {
	sum := md5.Sum(data) //nolint:gosec
	// We only want 48 bits as a float64 only has a 53 bit mantissa.
	smallSum := sum[0:6]
	bytes := make([]byte, 8) //nolint:gomnd
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
