package main

import (
	"context"

	"github.com/go-kit/kit/log"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

type StatefulSetWatcher struct {
	klient kubernetes.Interface
	logger log.Logger

	statefulsets map[string]StatefulSetUpdate
}

type StatefulSetUpdate struct {
	Name     string
	Replicas int
}

// Name will always be the same, as check outside of this func already
func (current StatefulSetUpdate) Changed(update StatefulSetUpdate) bool {
	return current.Replicas != update.Replicas
}

func (stsw *StatefulSetWatcher) Watch(ctx context.Context, namespace string, label string, updates chan<- StatefulSetUpdate) error {
	if stsw.logger == nil {
		stsw.logger = log.NewNopLogger()
	}
	if stsw.statefulsets == nil {
		stsw.statefulsets = make(map[string]StatefulSetUpdate)
	}

	stsWatch, err := stsw.klient.AppsV1().StatefulSets(namespace).Watch(metav1.ListOptions{
		LabelSelector: label,
	})
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			close(updates)
			stsWatch.Stop()
			return nil
		case event := <-stsWatch.ResultChan():
			switch event.Type {
			case watch.Modified, watch.Added:
				if event.Object == nil {
					continue
				}
				sts := event.Object.(*appsv1.StatefulSet)

				u := StatefulSetUpdate{
					Name:     sts.Name,
					Replicas: int(*sts.Spec.Replicas),
				}

				// Check if StatefulSet update is actually something we care about
				if current, exists := stsw.statefulsets[u.Name]; exists {
					if current.Changed(u) {
						stsw.statefulsets[u.Name] = u
						updates <- u
					}

					continue
				}

				stsw.statefulsets[u.Name] = u
				updates <- u

			case watch.Deleted:
			}
		}
	}
}
