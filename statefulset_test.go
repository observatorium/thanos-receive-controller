package main

import "testing"

func TestStatefulSetUpdate_Changed(t *testing.T) {
	tests := []struct {
		name    string
		current StatefulSetUpdate
		update  StatefulSetUpdate
		changed bool
	}{{
		name:    "unchanged",
		current: StatefulSetUpdate{Name: "foobar", Replicas: 42},
		update:  StatefulSetUpdate{Name: "foobar", Replicas: 42},
		changed: false,
	}, {
		name:    "changed",
		current: StatefulSetUpdate{Name: "foobar", Replicas: 23},
		update:  StatefulSetUpdate{Name: "foobar", Replicas: 42},
		changed: true,
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.current.Changed(tt.update) != tt.changed {
				if tt.changed {
					t.Errorf("expected to be changed")
				} else {
					t.Errorf("expected the same")
				}
			}
		})
	}
}
