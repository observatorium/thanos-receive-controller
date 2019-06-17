package main

import (
	"flag"
	"fmt"
	stdlog "log"

	"github.com/oklog/run"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	var gr run.Group
	{
		// Watch the original ConfigMap
	}
	{
		konfig, err := clientcmd.BuildConfigFromFlags("", config.KubeConfig)
		if err != nil {
			stdlog.Fatal(err)
		}

		klient, err := kubernetes.NewForConfig(konfig)
		if err != nil {
			stdlog.Fatal(err)
		}

		controller := &Controller{
			klient,
		}

		gr.Add(func() error {
			return controller.Run(config.Namespace, config.StatefulSetLabel)
		}, func(err error) {
			controller.Shutdown()
		})
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
	}

	for _, sts := range list.Items {
		r := sts.Spec.Replicas
		fmt.Println(r)
	}

	return nil
}

func (c *Controller) Shutdown() {

}
