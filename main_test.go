package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/improbable-eng/thanos/pkg/receive"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func intPointer(i int32) *int32 {
	return &i
}

func TestController(t *testing.T) {
	for _, tt := range []struct {
		name         string
		hashrings    []receive.HashringConfig
		statefulsets []*appsv1.StatefulSet
		expected     []receive.HashringConfig
	}{
		{
			name:     "Empty",
			expected: nil,
		},
		{
			name: "OneHashringNoStatefulSet",
			hashrings: []receive.HashringConfig{{
				Hashring: "hashring0",
			}},
			expected: []receive.HashringConfig{{Hashring: "hashring0"}},
		},
		{
			name: "OneHashringOneStatefulSetNoMatch",
			hashrings: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar"},
			}},
			statefulsets: []*appsv1.StatefulSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "hashring0",
						Labels: map[string]string{"a": "b"},
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas:    intPointer(3),
						ServiceName: "h0",
					},
				},
			},
			expected: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar"},
			}},
		},
		{
			name: "OneHashringOneStatefulSet",
			hashrings: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar"},
			}},
			statefulsets: []*appsv1.StatefulSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "thanos-receive-hashring0",
						Labels: map[string]string{
							"a":              "b",
							hashringLabelKey: "hashring0",
						},
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas:    intPointer(3),
						ServiceName: "h0",
					},
				},
			},
			expected: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar"},
				Endpoints: []string{
					"http://thanos-receive-hashring0-0.h0.namespace.svc.cluster.local:19291/api/v1/receive",
					"http://thanos-receive-hashring0-1.h0.namespace.svc.cluster.local:19291/api/v1/receive",
					"http://thanos-receive-hashring0-2.h0.namespace.svc.cluster.local:19291/api/v1/receive",
				},
			}},
		},
		{
			name: "OneHashringOneStatefulSetOneBadStatefulSet",
			hashrings: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar"},
			}},
			statefulsets: []*appsv1.StatefulSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "hashring0",
						Labels: map[string]string{
							"a":              "b",
							hashringLabelKey: "hashring0",
						},
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas:    intPointer(3),
						ServiceName: "h0",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "hashring123",
						Labels: map[string]string{
							"a":              "b",
							hashringLabelKey: "hashring123",
						},
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas:    intPointer(123),
						ServiceName: "h123",
					},
				},
			},
			expected: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar"},
				Endpoints: []string{
					"http://hashring0-0.h0.namespace.svc.cluster.local:19291/api/v1/receive",
					"http://hashring0-1.h0.namespace.svc.cluster.local:19291/api/v1/receive",
					"http://hashring0-2.h0.namespace.svc.cluster.local:19291/api/v1/receive",
				},
			}},
		},
		{
			name: "OneHashringManyStatefulSets",
			hashrings: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar"},
			}, {
				Hashring: "hashring1",
			}},
			statefulsets: []*appsv1.StatefulSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "hashring0",
						Labels: map[string]string{
							"a":              "b",
							hashringLabelKey: "hashring0",
						},
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas:    intPointer(3),
						ServiceName: "h0",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "hashring1",
						Labels: map[string]string{
							"a":              "b",
							hashringLabelKey: "hashring1",
						},
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas:    intPointer(2),
						ServiceName: "h1",
					},
				},
			},
			expected: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar"},
				Endpoints: []string{
					"http://hashring0-0.h0.namespace.svc.cluster.local:19291/api/v1/receive",
					"http://hashring0-1.h0.namespace.svc.cluster.local:19291/api/v1/receive",
					"http://hashring0-2.h0.namespace.svc.cluster.local:19291/api/v1/receive",
				},
			}, {
				Hashring: "hashring1",
				Endpoints: []string{
					"http://hashring1-0.h1.namespace.svc.cluster.local:19291/api/v1/receive",
					"http://hashring1-1.h1.namespace.svc.cluster.local:19291/api/v1/receive",
				},
			}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			klient := fake.NewSimpleClientset()
			opts := &options{
				labelKey:               "a",
				labelValue:             "b",
				fileName:               "hashrings.json",
				clusterDomain:          "cluster.local",
				configMapName:          "original",
				configMapGeneratedName: "generated",
				namespace:              "namespace",
				path:                   "/api/v1/receive",
				port:                   19291,
				scheme:                 "http",
			}
			c := newController(klient, nil, opts)
			stop := make(chan struct{})
			defer close(stop)

			go func() {
				if err := c.run(stop); err != nil {
					t.Fatalf("got unexpected error: %v", err)
				}
			}()

			buf, err := json.Marshal(tt.hashrings)
			if err != nil {
				t.Fatalf("got unexpected error marshaling initial hashrings: %v", err)
			}
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      opts.configMapName,
					Namespace: opts.namespace,
				},
				Data: map[string]string{
					opts.fileName: string(buf),
				},
				BinaryData: nil,
			}
			if _, err := klient.CoreV1().ConfigMaps(opts.namespace).Create(cm); err != nil {
				t.Fatalf("got unexpected error creating ConfigMap: %v", err)
			}

			for _, sts := range tt.statefulsets {
				if _, err := klient.AppsV1().StatefulSets(opts.namespace).Create(sts); err != nil {
					t.Fatalf("got unexpected error creating StatefulSet: %v", err)
				}
			}

			// Reconciliation is async, so we need to wait a bit.
			<-time.After(500 * time.Millisecond)
			cm, err = klient.CoreV1().ConfigMaps(opts.namespace).Get(opts.configMapGeneratedName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("got unexpected error getting ConfigMap: %v", err)
			}

			buf, err = json.Marshal(tt.expected)
			if err != nil {
				t.Fatalf("got unexpected error marshaling expected hashrings: %v", err)
			}

			if cm.Data[opts.fileName] != string(buf) {
				t.Errorf("the expected config does not match the actual config\ncase:\t%q\ngiven:\t%+v\nexpected:\t%+v\n", tt.name, cm.Data[opts.fileName], string(buf))
			}
		})
	}
}
