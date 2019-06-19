package main

import (
	"context"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/improbable-eng/thanos/pkg/receive"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

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
			close(updates)
			watch.Stop()
			return nil
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

type ConfigMapSaver struct {
	klient    kubernetes.Interface
	logger    log.Logger
	namespace string
	name      string
}

func (cms *ConfigMapSaver) SaveHashring(hashring []receive.HashringConfig) error {
	if cms.logger == nil {
		cms.logger = log.NewNopLogger()
	}

	bytes, err := yaml.Marshal(hashring)
	if err != nil {
		return err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cms.name,
			Namespace: cms.namespace,
		},
		Data: map[string]string{
			"config.yaml": string(bytes),
		},
		BinaryData: nil,
	}

	_, err = cms.klient.CoreV1().ConfigMaps(cms.namespace).Get(cms.name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err := cms.klient.CoreV1().ConfigMaps(cms.namespace).Create(cm)
		if err != nil {
			return err
		}
	}
	if err != nil {
		return nil
	}

	_, err = cms.klient.CoreV1().ConfigMaps(cms.namespace).Update(cm)
	return err
}
