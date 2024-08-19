package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/thanos/pkg/receive"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

const reconciliationDelay = 500 * time.Millisecond

func intPointer(i int32) *int32 {
	return &i
}

// nolint:funlen, maintidx
func TestController(t *testing.T) {
	ctx := context.Background()
	port := 10901

	for _, tt := range []struct {
		name          string
		hashrings     []receive.HashringConfig
		statefulsets  []*appsv1.StatefulSet
		clusterDomain string
		expected      []receive.HashringConfig
		notProvision  bool
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
			clusterDomain: "cluster.local",
			expected:      []receive.HashringConfig{{Hashring: "hashring0"}},
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
			clusterDomain: "cluster.local",
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
			clusterDomain: "cluster.local",
			expected: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar"},
				Endpoints: []receive.Endpoint{
					{Address: "thanos-receive-hashring0-0.h0.namespace.svc.cluster.local:10901"},
					{Address: "thanos-receive-hashring0-1.h0.namespace.svc.cluster.local:10901"},
					{Address: "thanos-receive-hashring0-2.h0.namespace.svc.cluster.local:10901"},
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
			clusterDomain: "cluster.local",
			expected: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar"},
				Endpoints: []receive.Endpoint{
					{Address: "hashring0-0.h0.namespace.svc.cluster.local:10901"},
					{Address: "hashring0-1.h0.namespace.svc.cluster.local:10901"},
					{Address: "hashring0-2.h0.namespace.svc.cluster.local:10901"},
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
			clusterDomain: "cluster.local",
			expected: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar"},
				Endpoints: []receive.Endpoint{
					{Address: "hashring0-0.h0.namespace.svc.cluster.local:10901"},
					{Address: "hashring0-1.h0.namespace.svc.cluster.local:10901"},
					{Address: "hashring0-2.h0.namespace.svc.cluster.local:10901"},
				},
			}, {
				Hashring: "hashring1",
				Endpoints: []receive.Endpoint{
					{Address: "hashring1-0.h1.namespace.svc.cluster.local:10901"},
					{Address: "hashring1-1.h1.namespace.svc.cluster.local:10901"},
				},
			}},
		},
		{
			name: "OneHashringLabelKeyManyStatefulSets",
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
						Name: "hashring1",
						Labels: map[string]string{
							"a":              "b",
							hashringLabelKey: "hashring0",
						},
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas:    intPointer(2),
						ServiceName: "h0",
					},
				},
			},
			clusterDomain: "cluster.local",
			expected: []receive.HashringConfig{
				{
					Hashring: "hashring0",
					Tenants:  []string{"foo", "bar"},
					Endpoints: []receive.Endpoint{
						{Address: "hashring0-0.h0.namespace.svc.cluster.local:10901"},
						{Address: "hashring0-1.h0.namespace.svc.cluster.local:10901"},
						{Address: "hashring0-2.h0.namespace.svc.cluster.local:10901"},
						{Address: "hashring1-0.h0.namespace.svc.cluster.local:10901"},
						{Address: "hashring1-1.h0.namespace.svc.cluster.local:10901"},
					},
				},
			},
		},
		{
			clusterDomain: "",
			name:          "OneHashringOneStatefulSetNoClusterDomain",
			hashrings: []receive.HashringConfig{{
				Hashring: "hashring1",
				Tenants:  []string{"fooo", "bar"},
			}},
			statefulsets: []*appsv1.StatefulSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "thanos-receive-hashring1",
						Labels: map[string]string{
							"a":              "b",
							hashringLabelKey: "hashring1",
						},
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas:    intPointer(3),
						ServiceName: "h0",
					},
				},
			},
			expected: []receive.HashringConfig{{
				Hashring: "hashring1",
				Tenants:  []string{"fooo", "bar"},
				Endpoints: []receive.Endpoint{
					{Address: "thanos-receive-hashring1-0.h0.namespace.svc:10901"},
					{Address: "thanos-receive-hashring1-1.h0.namespace.svc:10901"},
					{Address: "thanos-receive-hashring1-2.h0.namespace.svc:10901"},
				},
			}},
		},
	} {
		name := tt.name
		hashrings := tt.hashrings
		statefulsets := tt.statefulsets
		expected := tt.expected
		clusterDomain := tt.clusterDomain
		provisioned := !tt.notProvision

		t.Run(name, func(t *testing.T) {
			opts := &options{
				labelKey:               "a",
				labelValue:             "b",
				fileName:               "hashrings.json",
				clusterDomain:          clusterDomain,
				configMapName:          "original",
				configMapGeneratedName: "generated",
				namespace:              "namespace",
				port:                   port,
				scheme:                 "http",
			}
			klient := fake.NewSimpleClientset()
			cleanUp := setupController(ctx, t, klient, opts)
			defer cleanUp()

			_ = createInitialResources(ctx, t, klient, opts, hashrings, statefulsets, provisioned)

			// Reconciliation is async, so we need to wait a bit.
			<-time.After(reconciliationDelay)
			cm, err := klient.CoreV1().ConfigMaps(opts.namespace).Get(ctx, opts.configMapGeneratedName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("got unexpected error getting ConfigMap: %v", err)
			}

			buf, err := json.Marshal(expected)
			if err != nil {
				t.Fatalf("got unexpected error marshaling expected hashrings: %v", err)
			}

			if cm.Data[opts.fileName] != string(buf) {
				t.Errorf("the expected config does not match the actual config\ncase:\t%q\ngiven:\t%+v\nexpected:\t%+v\n", name, cm.Data[opts.fileName], string(buf))
			}
		})
	}
}

func TestControllerConfigmapUpdate(t *testing.T) {
	ctx := context.Background()
	port := 10901
	originalHashrings := []receive.HashringConfig{{
		Hashring: "hashring0",
		Tenants:  []string{"foo", "bar"},
		Endpoints: []receive.Endpoint{
			{Address: "thanos-receive-hashring0-0.h0.namespace.svc.cluster.local:10901"},
			{Address: "thanos-receive-hashring0-1.h0.namespace.svc.cluster.local:10901"},
			{Address: "thanos-receive-hashring0-2.h0.namespace.svc.cluster.local:10901"},
		},
	}}
	intendedLabels := map[string]string{
		"manual": "change",
	}

	for _, tt := range []struct {
		name            string
		hashrings       []receive.HashringConfig
		labels          map[string]string
		shouldBeUpdated bool
		provisioned     bool
	}{
		{
			name: "DifferentHashring",
			hashrings: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar", "baz"},
			}},
			labels:          intendedLabels,
			shouldBeUpdated: true,
		},
		{
			name:            "KeepLabels",
			hashrings:       originalHashrings,
			labels:          intendedLabels,
			shouldBeUpdated: false,
		},
	} {
		name := tt.name
		hashrings := tt.hashrings
		labels := tt.labels
		shouldBeUpdated := tt.shouldBeUpdated
		provisioned := tt.provisioned

		t.Run(name, func(t *testing.T) {
			opts := &options{
				labelKey:               "a",
				labelValue:             "b",
				fileName:               "hashrings.json",
				clusterDomain:          "cluster.local",
				configMapName:          "original",
				configMapGeneratedName: "generated",
				namespace:              "namespace",
				port:                   port,
				scheme:                 "http",
			}
			klient := fake.NewSimpleClientset()

			cm := createInitialResources(ctx, t, klient, opts,
				originalHashrings,
				[]*appsv1.StatefulSet{
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
				}, provisioned)

			buf, err := json.Marshal(hashrings)
			if err != nil {
				t.Fatalf("got unexpected error marshaling hashrings: %v", err)
			}
			gcm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      opts.configMapGeneratedName,
					Namespace: opts.namespace,
					Labels:    labels,
				},
				Data: map[string]string{
					opts.fileName: string(buf),
				},
			}
			if _, err = klient.CoreV1().ConfigMaps(opts.namespace).Create(ctx, gcm, metav1.CreateOptions{}); err != nil {
				t.Fatalf("got unexpected error creating ConfigMap: %v", err)
			}

			cleanUp := setupController(ctx, t, klient, opts)
			defer cleanUp()

			// Reconciliation is async, so we need to wait a bit.
			<-time.After(reconciliationDelay)
			gcm, err = klient.CoreV1().ConfigMaps(opts.namespace).Get(ctx, opts.configMapGeneratedName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("got unexpected error getting ConfigMap: %v", err)
			}

			if shouldBeUpdated {
				// Generated configmap should be overfidden by default properties.
				if cmp.Equal(gcm.Labels, map[string]string{}) {
					t.Errorf("generated configmap should have been updated\ncase:\t%q\noriginal:\t%+v\ngenerated:\t%+v\n", name, cm.Labels, gcm.Labels)
				}
			} else {
				// Generated configmap should preserve changes.
				if !cmp.Equal(gcm.Labels, labels) {
					t.Errorf("generated configmap should NOT have been updated\ncase:\t%q\noriginal:\t%+v\ngenerated:\t%+v\n", name, cm.Labels, gcm.Labels)
				}
			}
		})
	}
}

// nolint:funlen, maintidx
func TestControllerWithAzAware(t *testing.T) {
	ctx := context.Background()
	port := 10901

	for _, tt := range []struct {
		name          string
		hashrings     []receive.HashringConfig
		statefulsets  []*appsv1.StatefulSet
		clusterDomain string
		notProvision  bool
		expected      []receive.HashringConfig
		expectErr     bool
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
			clusterDomain: "cluster.local",
			expected:      []receive.HashringConfig{{Hashring: "hashring0"}},
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
			clusterDomain: "cluster.local",
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
			clusterDomain: "cluster.local",
			expected: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar"},
				Endpoints: []receive.Endpoint{
					{
						Address: "thanos-receive-hashring0-0.h0.namespace.svc.cluster.local:10901",
						AZ:      "thanos-receive-hashring0",
					},
					{
						Address: "thanos-receive-hashring0-1.h0.namespace.svc.cluster.local:10901",
						AZ:      "thanos-receive-hashring0",
					},
					{
						Address: "thanos-receive-hashring0-2.h0.namespace.svc.cluster.local:10901",
						AZ:      "thanos-receive-hashring0",
					},
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
			clusterDomain: "cluster.local",
			expected: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar"},
				Endpoints: []receive.Endpoint{
					{
						Address: "hashring0-0.h0.namespace.svc.cluster.local:10901",
						AZ:      "hashring0",
					},
					{
						Address: "hashring0-1.h0.namespace.svc.cluster.local:10901",
						AZ:      "hashring0",
					},
					{
						Address: "hashring0-2.h0.namespace.svc.cluster.local:10901",
						AZ:      "hashring0",
					},
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
			clusterDomain: "cluster.local",
			expected: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar"},
				Endpoints: []receive.Endpoint{
					{
						Address: "hashring0-0.h0.namespace.svc.cluster.local:10901",
						AZ:      "hashring0",
					},
					{
						Address: "hashring0-1.h0.namespace.svc.cluster.local:10901",
						AZ:      "hashring0",
					},
					{
						Address: "hashring0-2.h0.namespace.svc.cluster.local:10901",
						AZ:      "hashring0",
					},
				},
			}, {
				Hashring: "hashring1",
				Endpoints: []receive.Endpoint{
					{
						Address: "hashring1-0.h1.namespace.svc.cluster.local:10901",
						AZ:      "hashring1",
					},
					{
						Address: "hashring1-1.h1.namespace.svc.cluster.local:10901",
						AZ:      "hashring1",
					},
				},
			}},
		},
		{
			name: "OneHashringLabelKeyManyStatefulSets",
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
						Name: "hashring1",
						Labels: map[string]string{
							"a":              "b",
							hashringLabelKey: "hashring0",
						},
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas:    intPointer(2),
						ServiceName: "h0",
					},
				},
			},
			clusterDomain: "cluster.local",
			expected: []receive.HashringConfig{
				{
					Hashring: "hashring0",
					Tenants:  []string{"foo", "bar"},
					Endpoints: []receive.Endpoint{
						{
							Address: "hashring0-0.h0.namespace.svc.cluster.local:10901",
							AZ:      "hashring0",
						},
						{
							Address: "hashring0-1.h0.namespace.svc.cluster.local:10901",
							AZ:      "hashring0",
						},
						{
							Address: "hashring0-2.h0.namespace.svc.cluster.local:10901",
							AZ:      "hashring0",
						},
						{
							Address: "hashring1-0.h0.namespace.svc.cluster.local:10901",
							AZ:      "hashring1",
						},
						{
							Address: "hashring1-1.h0.namespace.svc.cluster.local:10901",
							AZ:      "hashring1",
						},
					},
				},
			},
		},
		{
			clusterDomain: "",
			name:          "OneHashringOneStatefulSetNoClusterDomain",
			hashrings: []receive.HashringConfig{{
				Hashring: "hashring1",
				Tenants:  []string{"fooo", "bar"},
			}},
			statefulsets: []*appsv1.StatefulSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "thanos-receive-hashring1",
						Labels: map[string]string{
							"a":              "b",
							hashringLabelKey: "hashring1",
						},
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas:    intPointer(3),
						ServiceName: "h0",
					},
				},
			},
			expected: []receive.HashringConfig{{
				Hashring: "hashring1",
				Tenants:  []string{"fooo", "bar"},
				Endpoints: []receive.Endpoint{
					{
						Address: "thanos-receive-hashring1-0.h0.namespace.svc:10901",
						AZ:      "thanos-receive-hashring1",
					},
					{
						Address: "thanos-receive-hashring1-1.h0.namespace.svc:10901",
						AZ:      "thanos-receive-hashring1",
					},
					{
						Address: "thanos-receive-hashring1-2.h0.namespace.svc:10901",
						AZ:      "thanos-receive-hashring1",
					},
				},
			}},
		},
		{
			name: "OneHashringManyStatefulSetsNotProvisionedError",
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
						Name: "hashring1",
						Labels: map[string]string{
							"a":              "b",
							hashringLabelKey: "hashring0",
						},
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas:    intPointer(2),
						ServiceName: "h0",
					},
				},
			},
			clusterDomain: "cluster.local",
			notProvision:  true,
			expectErr:     true,
		},
		{
			name:         "OneHashringManyStatefulSetsNotProvisioned",
			notProvision: true,
			hashrings: []receive.HashringConfig{{
				Hashring: "group",
			}},
			statefulsets: []*appsv1.StatefulSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rep0",
						Labels: map[string]string{
							"a":              "b",
							hashringLabelKey: "group",
						},
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas:    intPointer(1),
						ServiceName: "h0",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rep1",
						Labels: map[string]string{
							"a":              "b",
							hashringLabelKey: "group",
						},
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas:    intPointer(1),
						ServiceName: "h1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rep2",
						Labels: map[string]string{
							"a":              "b",
							hashringLabelKey: "group",
						},
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas:    intPointer(1),
						ServiceName: "h2",
					},
				},
			},
			clusterDomain: "cluster.local",
			expected: []receive.HashringConfig{{
				Hashring: "group",
				Endpoints: []receive.Endpoint{
					{
						Address: "rep0-0.h0.namespace.svc.cluster.local:10901",
						AZ:      "rep0",
					},
					{
						Address: "rep1-0.h1.namespace.svc.cluster.local:10901",
						AZ:      "rep1",
					},
					{
						Address: "rep2-0.h2.namespace.svc.cluster.local:10901",
						AZ:      "rep2",
					},
				},
			}},
		},
	} {
		name := tt.name
		hashrings := tt.hashrings
		statefulsets := tt.statefulsets
		expected := tt.expected
		expectErr := tt.expectErr
		clusterDomain := tt.clusterDomain
		provisioned := !tt.notProvision

		t.Run(name, func(t *testing.T) {
			opts := &options{
				labelKey:               "a",
				labelValue:             "b",
				fileName:               "hashrings.json",
				clusterDomain:          clusterDomain,
				configMapName:          "original",
				configMapGeneratedName: "generated",
				namespace:              "namespace",
				port:                   port,
				scheme:                 "http",
				useAzAwareHashRing:     true,
				migrationState:         "hello",
			}
			klient := fake.NewSimpleClientset()
			cleanUp := setupController(ctx, t, klient, opts)
			defer cleanUp()

			_ = createInitialResources(ctx, t, klient, opts, hashrings, statefulsets, provisioned)

			// Reconciliation is async, so we need to wait a bit.
			<-time.After(reconciliationDelay)
			cm, err := klient.CoreV1().ConfigMaps(opts.namespace).Get(ctx, opts.configMapGeneratedName, metav1.GetOptions{})
			if expectErr {
				require.Error(t, err, "expected error to get generated config map")
				return
			}
			if err != nil {
				t.Fatalf("got unexpected error getting ConfigMap: %v", err)
			}

			buf, err := json.Marshal(expected)
			if err != nil {
				t.Fatalf("got unexpected error marshaling expected hashrings: %v", err)
			}

			if cm.Data[opts.fileName] != string(buf) {
				t.Errorf("the expected config does not match the actual config\ncase:\t%q\ngiven:\t%+v\nexpected:\t%+v\n", name, cm.Data[opts.fileName], string(buf))
			}
		})
	}
}

func TestControllerConfigmapUpdateWithAzAware(t *testing.T) {
	ctx := context.Background()
	port := 10901
	originalHashrings := []receive.HashringConfig{{
		Hashring: "hashring0",
		Tenants:  []string{"foo", "bar"},
		Endpoints: []receive.Endpoint{
			{
				Address: "thanos-receive-hashring0-0.h0.namespace.svc.cluster.local:10901",
				AZ:      "thanos-receive-hashring0",
			},
			{
				Address: "thanos-receive-hashring0-1.h0.namespace.svc.cluster.local:10901",
				AZ:      "thanos-receive-hashring0",
			},
			{
				Address: "thanos-receive-hashring0-2.h0.namespace.svc.cluster.local:10901",
				AZ:      "thanos-receive-hashring0",
			},
		},
	}}
	intendedLabels := map[string]string{
		"manual": "change",
	}

	for _, tt := range []struct {
		name            string
		hashrings       []receive.HashringConfig
		labels          map[string]string
		shouldBeUpdated bool
		provisioned     bool
	}{
		{
			name: "DifferentHashring",
			hashrings: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar", "baz"},
			}},
			labels:          intendedLabels,
			shouldBeUpdated: true,
		},
		{
			name:            "KeepLabels",
			hashrings:       originalHashrings,
			labels:          intendedLabels,
			shouldBeUpdated: false,
		},
	} {
		name := tt.name
		hashrings := tt.hashrings
		labels := tt.labels
		shouldBeUpdated := tt.shouldBeUpdated
		provisioned := tt.provisioned

		t.Run(name, func(t *testing.T) {
			opts := &options{
				labelKey:               "a",
				labelValue:             "b",
				fileName:               "hashrings.json",
				clusterDomain:          "cluster.local",
				configMapName:          "original",
				configMapGeneratedName: "generated",
				namespace:              "namespace",
				port:                   port,
				scheme:                 "http",
				useAzAwareHashRing:     true,
			}
			klient := fake.NewSimpleClientset()

			cm := createInitialResources(ctx, t, klient, opts,
				originalHashrings,
				[]*appsv1.StatefulSet{
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
				}, provisioned)

			buf, err := json.Marshal(hashrings)
			if err != nil {
				t.Fatalf("got unexpected error marshaling hashrings: %v", err)
			}
			gcm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      opts.configMapGeneratedName,
					Namespace: opts.namespace,
					Labels:    labels,
				},
				Data: map[string]string{
					opts.fileName: string(buf),
				},
			}
			if _, err = klient.CoreV1().ConfigMaps(opts.namespace).Create(ctx, gcm, metav1.CreateOptions{}); err != nil {
				t.Fatalf("got unexpected error creating ConfigMap: %v", err)
			}

			cleanUp := setupController(ctx, t, klient, opts)
			defer cleanUp()

			// Reconciliation is async, so we need to wait a bit.
			<-time.After(reconciliationDelay)
			gcm, err = klient.CoreV1().ConfigMaps(opts.namespace).Get(ctx, opts.configMapGeneratedName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("got unexpected error getting ConfigMap: %v", err)
			}

			if shouldBeUpdated {
				// Generated configmap should be overfidden by default properties.
				if cmp.Equal(gcm.Labels, map[string]string{}) {
					t.Errorf("generated configmap should have been updated\ncase:\t%q\noriginal:\t%+v\ngenerated:\t%+v\n", name, cm.Labels, gcm.Labels)
				}
			} else {
				// Generated configmap should preserve changes.
				if !cmp.Equal(gcm.Labels, labels) {
					t.Errorf("generated configmap should NOT have been updated\ncase:\t%q\noriginal:\t%+v\ngenerated:\t%+v\n", name, cm.Labels, gcm.Labels)
				}
			}
		})
	}
}

func setupController(ctx context.Context, t *testing.T, klient kubernetes.Interface, opts *options) func() {
	t.Helper()

	c := newController(klient, nil, opts)
	stop := make(chan struct{})

	//nolint:staticcheck
	go func() {
		if err := c.run(ctx, stop); err != nil {
			t.Errorf("got unexpected error: %v", err)
		}
	}()

	return func() {
		close(stop)
	}
}

func createInitialResources(
	ctx context.Context,
	t *testing.T,
	klient kubernetes.Interface,
	opts *options,
	hashrings []receive.HashringConfig,
	statefulsets []*appsv1.StatefulSet,
	provisioned bool,
) *corev1.ConfigMap {
	t.Helper()

	buf, err := json.Marshal(hashrings)
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
	}
	if _, err := klient.CoreV1().ConfigMaps(opts.namespace).Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		t.Fatalf("got unexpected error creating ConfigMap: %v", err)
	}

	if provisioned {
		genCm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      opts.configMapGeneratedName,
				Namespace: opts.namespace,
			},
			Data: map[string]string{
				opts.fileName: "empty",
			},
		}
		if _, err := klient.CoreV1().ConfigMaps(opts.namespace).Create(ctx, genCm, metav1.CreateOptions{}); err != nil {
			t.Fatalf("got unexpected error creating GeneratedConfigMap: %v", err)
		}
	}

	for _, sts := range statefulsets {
		if _, err := klient.AppsV1().StatefulSets(opts.namespace).Create(ctx, sts, metav1.CreateOptions{}); err != nil {
			t.Fatalf("got unexpected error creating StatefulSet: %v", err)
		}
	}

	return cm
}
