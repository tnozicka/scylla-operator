// Copyright (c) 2023 ScyllaDB.

package algorithms_test

import (
	"testing"

	"github.com/scylladb/scylla-operator/pkg/util/algorithms"
)

func TestMax(t *testing.T) {
	t.Parallel()

	tt := []struct {
		name        string
		values      []int
		expectedMax int
	}{
		{
			name:        "all equal",
			values:      []int{1, 1, 1, 1, 1},
			expectedMax: 1,
		},
		{
			name:        "max at the beginning",
			values:      []int{9, 1, 2, 3, 4},
			expectedMax: 9,
		},
		{
			name:        "max in the middle",
			values:      []int{1, 2, 9, 3, 4},
			expectedMax: 9,
		},
		{
			name:        "max at the end",
			values:      []int{1, 2, 3, 4, 9},
			expectedMax: 9,
		},
	}
	for i := range tt {
		tc := tt[i]

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if len(tc.values) < 2 {
				panic("at least two input values must be provided")
			}
			got := algorithms.Max(tc.values[0], tc.values[1:]...)
			if got != tc.expectedMax {
				t.Errorf("expected %v, got %v", tc.expectedMax, got)
			}
		})
	}
}
