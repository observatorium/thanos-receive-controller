package main

import (
	"encoding/json"
	"flag"
	"fmt"
	stdlog "log"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/improbable-eng/thanos/pkg/receive"
	"github.com/oklog/run"
	"github.com/pkg/errors"
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

const (
	resyncPeriod = 5 * time.Minute
)

func main() {
	config := struct {
		KubeConfig             string
		Namespace              string
		StatefulSetLabel       string
		ConfigMapName          string
		ConfigMapGeneratedName string
		FileName               string
		Path                   string
		Port                   int
		Scheme                 string
	}{}

	flag.StringVar(&config.KubeConfig, "kubeconfig", "", "Path to kubeconfig")
	flag.StringVar(&config.Namespace, "namespace", "default", "The namespace to watch")
	flag.StringVar(&config.StatefulSetLabel, "statefulset-label", "controller.receive.thanos.io=thanos-receive-controller", "The label StatefulSets must have to be watched by the controller")
	flag.StringVar(&config.ConfigMapName, "configmap-name", "", "The name of the original ConfigMap containing the hashring tenant configuration")
	flag.StringVar(&config.ConfigMapGeneratedName, "configmap-generated-name", "", "The name of the generated and populated ConfigMap")
	flag.StringVar(&config.FileName, "file-name", "", "The name of the configuration file in the ConfigMap")
	flag.StringVar(&config.Path, "path", "/api/v1/receive", "The URL path on which receive components accept write requests")
	flag.IntVar(&config.Port, "port", 19291, "The port on which receive components are listening for write requests")
	flag.StringVar(&config.Scheme, "scheme", "http", "The URL scheme on which receive components accept write requests")
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

	var g run.Group
	{
		// Signal chans must be buffered.
		sig := make(chan os.Signal, 1)
		g.Add(func() error {
			signal.Notify(sig, os.Interrupt, os.Kill)
			<-sig
			return nil
		}, func(err error) {
			level.Info(logger).Log("msg", "caught interrrupt")
			close(sig)
		})
	}
	{
		opt := &options{
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
		done := make(chan struct{})

		g.Add(func() error {
			return c.run(done)
		}, func(err error) {
			level.Info(logger).Log("msg", "shutting down controller")
			close(done)
		})
	}
	level.Info(logger).Log("msg", "starting the controller")

	if err := g.Run(); err != nil {
		stdlog.Fatal(err)
	}
}

type options struct {
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
	queue   chan struct{}
	logger  log.Logger

	klient  kubernetes.Interface
	cmapInf cache.SharedIndexInformer
	ssetInf cache.SharedIndexInformer
}

func newController(klient kubernetes.Interface, logger log.Logger, o *options) *controller {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return &controller{
		options: o,
		queue:   make(chan struct{}),
		logger:  logger,

		klient:  klient,
		cmapInf: coreinformers.NewConfigMapInformer(klient, o.namespace, resyncPeriod, nil),
		ssetInf: appsinformers.NewFilteredStatefulSetInformer(klient, o.namespace, resyncPeriod, nil, func(lo *v1.ListOptions) {
			lo.LabelSelector = labels.Set{o.labelKey: o.labelValue}.String()
		}),
	}
}

func (c *controller) run(stopc <-chan struct{}) error {
	defer close(c.queue)

	go c.worker()

	go c.cmapInf.Run(stopc)
	go c.ssetInf.Run(stopc)
	if err := c.waitForCacheSync(stopc); err != nil {
		return err
	}
	c.cmapInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ interface{}) { c.addWorkItem() },
		DeleteFunc: func(_ interface{}) { c.addWorkItem() },
		UpdateFunc: func(_, _ interface{}) { c.addWorkItem() },
	})
	c.ssetInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ interface{}) { c.addWorkItem() },
		DeleteFunc: func(_ interface{}) { c.addWorkItem() },
		UpdateFunc: func(_, _ interface{}) { c.addWorkItem() },
	})

	<-stopc
	return nil
}

// addWorkItem is non-blocking. If the chan is blocked,
// then an update will already be processed and we don't
// need to notify again.
func (c *controller) addWorkItem() {
	select {
	case c.queue <- struct{}{}:
		return
	default:
		return
	}
}

// waitForCacheSync waits for the informers' caches to be synced.
func (c *controller) waitForCacheSync(stopc <-chan struct{}) error {
	ok := true
	informers := []struct {
		name     string
		informer cache.SharedIndexInformer
	}{
		{"ConfigMap", c.cmapInf},
		{"StatefulSet", c.ssetInf},
	}
	for _, inf := range informers {
		if !cache.WaitForCacheSync(stopc, inf.informer.HasSynced) {
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

// worker runs a worker thread that just dequeues items, processes them, and
// marks them done.
func (c *controller) worker() {
	for range c.queue {
		c.sync()
	}
}

func (c *controller) sync() {
	configMap, ok, err := c.cmapInf.GetStore().GetByKey(fmt.Sprintf("%s/%s", c.options.namespace, c.options.configMapName))
	if !ok || err != nil {
		level.Warn(c.logger).Log("msg", "could not fetch ConfigMap", "err", err, "name", c.options.configMapName)
		return
	}
	cm := configMap.(*corev1.ConfigMap)

	var hashrings []receive.HashringConfig
	if err := json.Unmarshal([]byte(cm.Data[c.options.fileName]), &hashrings); err != nil {
		level.Warn(c.logger).Log("msg", "failed to decode configuration", "err", err)
		return
	}

	statefulsets := make(map[string]*appsv1.StatefulSet)
	for _, obj := range c.ssetInf.GetStore().List() {
		sts := obj.(*appsv1.StatefulSet)
		statefulsets[sts.Name] = sts
	}

	c.populate(hashrings, statefulsets)
	c.saveHashring(hashrings)
}

func (c *controller) populate(hashrings []receive.HashringConfig, statefulsets map[string]*appsv1.StatefulSet) {
	for i, h := range hashrings {
		if sts, exists := statefulsets[h.Hashring]; exists {
			var endpoints []string
			for i := 0; i < int(*sts.Spec.Replicas); i++ {
				endpoints = append(endpoints,
					// TODO: Make sure this is actually correct
					fmt.Sprintf("%s://%s-%d.%s.%s:%d/%s",
						c.options.scheme,
						sts.Name,
						i,
						sts.Name,
						c.options.namespace,
						c.options.port,
						strings.TrimPrefix(c.options.path, "/")),
				)
			}
			hashrings[i].Endpoints = endpoints
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

	_, err = c.klient.CoreV1().ConfigMaps(c.options.namespace).Get(c.options.configMapGeneratedName, metav1.GetOptions{})
	if kerrors.IsNotFound(err) {
		_, err = c.klient.CoreV1().ConfigMaps(c.options.namespace).Create(cm)
		return err
	}
	if err != nil {
		return err
	}

	_, err = c.klient.CoreV1().ConfigMaps(c.options.namespace).Update(cm)
	return err
}
