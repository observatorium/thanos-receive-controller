package main

import (
	"context"
	"flag"
	"fmt"
	stdlog "log"
	"os"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/improbable-eng/thanos/pkg/receive"
	"github.com/oklog/run"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	config := struct {
		KubeConfig             string
		Namespace              string
		StatefulSetLabel       string
		ConfigMapName          string
		ConfigMapGeneratedName string
	}{}

	flag.StringVar(&config.KubeConfig, "kubeconfig", "", "Path to kubeconfig")
	flag.StringVar(&config.Namespace, "namespace", "default", "The namespace we operate in")
	flag.StringVar(&config.StatefulSetLabel, "statefulset-label", "hashring", "The label StatefulSets have to be watched with")
	flag.StringVar(&config.ConfigMapName, "configmap-name", "", "The name to the original ConfigMap containing all hashring tenants")
	flag.StringVar(&config.ConfigMapGeneratedName, "configmap-generated-name", "", "The name to the generated and populated ConfigMap")
	flag.Parse()

	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	logger = log.WithPrefix(logger, "ts", log.DefaultTimestampUTC)
	logger = log.WithPrefix(logger, "caller", log.DefaultCaller)

	konfig, err := clientcmd.BuildConfigFromFlags("", config.KubeConfig)
	if err != nil {
		stdlog.Fatal(err)
	}

	klient, err := kubernetes.NewForConfig(konfig)
	if err != nil {
		stdlog.Fatal(err)
	}

	hashringUpdates := make(chan []receive.HashringConfig)

	var gr run.Group
	{
		ctx, cancel := context.WithCancel(context.Background())
		cmw := ConfigMapWatcher{klient: klient}

		gr.Add(func() error {
			return cmw.Watch(ctx, config.Namespace, config.ConfigMapName, hashringUpdates)
		}, func(err error) {
			cancel()
		})
	}
	{
		gr.Add(func() error {
			for hu := range hashringUpdates {
				for _, hashring := range hu {
					fmt.Printf("%+v\n", hashring)
				}
			}
			return nil
		}, func(err error) {
		})
	}
	//{
	//	controller := &Controller{
	//		klient,
	//	}
	//
	//	gr.Add(func() error {
	//		return controller.Run(config.Namespace, config.StatefulSetLabel)
	//	}, func(err error) {
	//		controller.Shutdown()
	//	})
	//}

	level.Info(logger).Log("msg", "starting the controller")

	if err := gr.Run(); err != nil {
		stdlog.Fatal(err)
	}
}

type ConfigMapWatcher struct {
	klient kubernetes.Interface
	logger log.Logger
}

func (cmw *ConfigMapWatcher) Watch(ctx context.Context, namespace string, name string, updates chan<- []receive.HashringConfig) error {
	if cmw.logger == nil {
		cmw.logger = log.NewNopLogger()
	}

	watch, err := cmw.klient.CoreV1().ConfigMaps(namespace).Watch(metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.name", name).String(),
		Watch:         true,
	})
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			watch.Stop()
			close(updates)
		case event := <-watch.ResultChan():
			cm := event.Object.(*corev1.ConfigMap)
			c := cm.Data["config.yaml"]

			var hashrings []receive.HashringConfig

			if err := yaml.Unmarshal([]byte(c), &hashrings); err != nil {
				level.Warn(cmw.logger).Log("msg", "failed to decode configuration", "err", err)
				return err
			}

			updates <- hashrings
		}
	}
}

type Controller struct {
	klient kubernetes.Interface
}

func (c *Controller) Run(namespace string, statefulSetLabel string) error {
	list, err := c.klient.AppsV1().StatefulSets(namespace).List(metav1.ListOptions{
		LabelSelector: statefulSetLabel,
	})
	if err != nil {
		return err
	}

	for _, sts := range list.Items {
		r := sts.Spec.Replicas
		fmt.Println(r)
	}

	return nil
}

func (c *Controller) Shutdown() {}
