package main

import (
	"reflect"
	"testing"

	"github.com/improbable-eng/thanos/pkg/receive"
)

func TestConfigPopulator(t *testing.T) {
	tests := []struct {
		name     string
		updates  func(*ConfigPopulator)
		expected []receive.HashringConfig
	}{
		{
			name:     "Empty",
			updates:  func(cp *ConfigPopulator) {},
			expected: nil,
		}, {
			name: "OneHashringNoStatefulSet",
			updates: func(cp *ConfigPopulator) {
				cp.UpdateConfig([]receive.HashringConfig{{
					Hashring: "hashring0",
				}})
			},
			expected: []receive.HashringConfig{{Hashring: "hashring0"}},
		},
		{
			name: "OneHashringOneStatefulSet",
			updates: func(cp *ConfigPopulator) {
				cp.UpdateConfig([]receive.HashringConfig{{
					Hashring:  "hashring0",
					Tenants:   []string{"foo", "bar"},
					Endpoints: nil,
				}})
				cp.UpdateStatefulSet(StatefulSetUpdate{
					Name:     "hashring0",
					Replicas: 3,
				})
			},
			expected: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar"},
				Endpoints: []string{
					"hashring0-0.hashring0.namespace:10901",
					"hashring0-1.hashring0.namespace:10901",
					"hashring0-2.hashring0.namespace:10901",
				},
			}},
		},
		{
			name: "OneHashringOneStatefulSetUpdated",
			updates: func(cp *ConfigPopulator) {
				cp.UpdateConfig([]receive.HashringConfig{{
					Hashring:  "hashring0",
					Tenants:   []string{"foo", "bar"},
					Endpoints: nil,
				}})
				cp.UpdateStatefulSet(StatefulSetUpdate{
					Name:     "hashring0",
					Replicas: 3,
				})
				cp.UpdateStatefulSet(StatefulSetUpdate{
					Name:     "hashring0",
					Replicas: 5,
				})
			},
			expected: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar"},
				Endpoints: []string{
					"hashring0-0.hashring0.namespace:10901",
					"hashring0-1.hashring0.namespace:10901",
					"hashring0-2.hashring0.namespace:10901",
					"hashring0-3.hashring0.namespace:10901",
					"hashring0-4.hashring0.namespace:10901",
				},
			}},
		},
		{
			name: "OneHashringUpdatedOneStatefulSet",
			updates: func(cp *ConfigPopulator) {
				cp.UpdateConfig([]receive.HashringConfig{{
					Hashring:  "hashring0",
					Tenants:   []string{"foo", "bar"},
					Endpoints: nil,
				}})
				cp.UpdateStatefulSet(StatefulSetUpdate{
					Name:     "hashring0",
					Replicas: 3,
				})
				cp.UpdateConfig([]receive.HashringConfig{{
					Hashring:  "hashring0",
					Tenants:   []string{"foo", "bar", "baz"},
					Endpoints: nil,
				}})
			},
			expected: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar", "baz"},
				Endpoints: []string{
					"hashring0-0.hashring0.namespace:10901",
					"hashring0-1.hashring0.namespace:10901",
					"hashring0-2.hashring0.namespace:10901",
				},
			}},
		},
		{
			name: "OneHashringOneStatefulSetOneUnusedStatefulSet",
			updates: func(cp *ConfigPopulator) {
				cp.UpdateConfig([]receive.HashringConfig{{
					Hashring:  "hashring0",
					Tenants:   []string{"foo", "bar"},
					Endpoints: nil,
				}})
				cp.UpdateStatefulSet(StatefulSetUpdate{
					Name:     "hashring0",
					Replicas: 3,
				})
				cp.UpdateStatefulSet(StatefulSetUpdate{
					Name:     "hashring123",
					Replicas: 123,
				})
			},
			expected: []receive.HashringConfig{{
				Hashring: "hashring0",
				Tenants:  []string{"foo", "bar"},
				Endpoints: []string{
					"hashring0-0.hashring0.namespace:10901",
					"hashring0-1.hashring0.namespace:10901",
					"hashring0-2.hashring0.namespace:10901",
				},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cp := &ConfigPopulator{namespace: "namespace"}

			tt.updates(cp)

			config := cp.Populate()
			if !reflect.DeepEqual(config, tt.expected) {
				t.Errorf("the expected config does not match the actual config\ngiven:    %+v\nexpected: %+v\n", config, tt.expected)
			}
		})
	}
}
