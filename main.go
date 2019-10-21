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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/improbable-eng/thanos/pkg/receive"
	"github.com/oklog/run"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

type label = string

const (
	resyncPeriod                  = 5 * time.Minute
	internalServerShutdownTimeout = time.Second
	hashringLabelKey              = "controller.receive.thanos.io/hashring"

	// Metric label values
	fetch  label = "fetch"
	decode label = "decode"
	save   label = "save"
	create label = "create"
	update label = "update"
	other  label = "other"
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
		Path                   string
		Port                   int
		Scheme                 string
		InternalAddr           string
	}{}

	flag.StringVar(&config.KubeConfig, "kubeconfig", "", "Path to kubeconfig")
	flag.StringVar(&config.Namespace, "namespace", "default", "The namespace to watch")
	flag.StringVar(&config.StatefulSetLabel, "statefulset-label", "controller.receive.thanos.io=thanos-receive-controller", "The label StatefulSets must have to be watched by the controller")
	flag.StringVar(&config.ClusterDomain, "cluster-domain", "cluster.local", "The DNS domain of the cluster")
	flag.StringVar(&config.ConfigMapName, "configmap-name", "", "The name of the original ConfigMap containing the hashring tenant configuration")
	flag.StringVar(&config.ConfigMapGeneratedName, "configmap-generated-name", "", "The name of the generated and populated ConfigMap")
	flag.StringVar(&config.FileName, "file-name", "", "The name of the configuration file in the ConfigMap")
	flag.StringVar(&config.Path, "path", "/api/v1/receive", "The URL path on which receive components accept write requests")
	flag.IntVar(&config.Port, "port", 19291, "The port on which receive components are listening for write requests")
	flag.StringVar(&config.Scheme, "scheme", "http", "The URL scheme on which receive components accept write requests")
	flag.StringVar(&config.InternalAddr, "internal-addr", ":8080", "The address on which internal server runs")
	flag.Parse()

	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	logger = log.WithPrefix(logger, "ts", log.DefaultTimestampUTC)
	logger = log.WithPrefix(logger, "caller", log.DefaultCaller)

	parts := strings.Split(config.StatefulSetLabel, "=")
	if len(parts) != 2 {
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
			path:                   config.Path,
			port:                   config.Port,
			scheme:                 config.Scheme,
			labelKey:               labelKey,
			labelValue:             labelValue,
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
	path                   string
	port                   int
	scheme                 string
	labelKey               string
	labelValue             string
}

type controller struct {
	options *options
	queue   *queue
	logger  log.Logger

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

		klient:  klient,
		cmapInf: coreinformers.NewConfigMapInformer(klient, o.namespace, resyncPeriod, nil),
		ssetInf: appsinformers.NewFilteredStatefulSetInformer(klient, o.namespace, resyncPeriod, nil, func(lo *v1.ListOptions) {
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

func (c *controller) run(stop <-chan struct{}) error {
	defer c.queue.stop()

	go c.cmapInf.Run(stop)
	go c.ssetInf.Run(stop)
	if err := c.waitForCacheSync(stop); err != nil {
		return err
	}
	c.cmapInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ interface{}) { c.queue.add() },
		DeleteFunc: func(_ interface{}) { c.queue.add() },
		UpdateFunc: func(_, _ interface{}) { c.queue.add() },
	})
	c.ssetInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ interface{}) { c.queue.add() },
		DeleteFunc: func(_ interface{}) { c.queue.add() },
		UpdateFunc: func(_, _ interface{}) { c.queue.add() },
	})
	go c.worker()

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

func (c *controller) worker() {
	for c.queue.get() {
		c.sync()
	}
}

func (c *controller) sync() {
	c.reconcileAttempts.Inc()
	configMap, ok, err := c.cmapInf.GetStore().GetByKey(fmt.Sprintf("%s/%s", c.options.namespace, c.options.configMapName))
	if !ok || err != nil {
		c.reconcileErrors.WithLabelValues(fetch).Inc()
		level.Warn(c.logger).Log("msg", "could not fetch ConfigMap", "err", err, "name", c.options.configMapName)
		return
	}
	cm := configMap.(*corev1.ConfigMap)

	var hashrings []receive.HashringConfig
	if err := json.Unmarshal([]byte(cm.Data[c.options.fileName]), &hashrings); err != nil {
		c.reconcileErrors.WithLabelValues(decode).Inc()
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
		c.reconcileErrors.WithLabelValues(save).Inc()
		level.Error(c.logger).Log("msg", "failed to save hashrings")
	}
}

func (c *controller) populate(hashrings []receive.HashringConfig, statefulsets map[string]*appsv1.StatefulSet) {
	for i, h := range hashrings {
		if sts, exists := statefulsets[h.Hashring]; exists {
			var endpoints []string
			for i := 0; i < int(*sts.Spec.Replicas); i++ {
				endpoints = append(endpoints,
					fmt.Sprintf("%s://%s-%d.%s.%s.svc.%s:%d/%s",
						c.options.scheme,
						sts.Name,
						i,
						sts.Spec.ServiceName,
						c.options.namespace,
						c.options.clusterDomain,
						c.options.port,
						strings.TrimPrefix(c.options.path, "/")),
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

	_, err = c.klient.CoreV1().ConfigMaps(c.options.namespace).Update(cm)
	if err != nil {
		c.configmapChangeErrors.WithLabelValues(update).Inc()
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
