package chanevents

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestQuantile pins the interpolation contract and the error paths Quantile
// surfaces to callers.
func TestQuantile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		q         float64
		xs        []float64
		want      float64
		expectErr bool
	}{
		{
			name:      "empty slice",
			xs:        []float64{},
			expectErr: true,
		},
		{
			name: "single value",
			xs: []float64{
				1,
			},
			want: 1.0,
		},
		{
			name: "single value median",
			xs: []float64{
				1,
			},
			q:    0.5,
			want: 1.0,
		},
		{
			name:      "quantile out of bound below",
			xs:        []float64{},
			q:         -0.1,
			expectErr: true,
		},
		{
			name:      "quantile out of bound above",
			xs:        []float64{},
			q:         1.1,
			expectErr: true,
		},
		{
			name: "median odd values",
			q:    0.5,
			xs: []float64{
				1,
				2,
				3,
				4,
				5,
			},
			want: 3.0,
		},
		{
			name: "median even values",
			q:    0.5,
			xs: []float64{
				1,
				2,
				3,
				4,
			},
			want: 2.5,
		},
		{
			name: "median unsorted",
			q:    0.5,
			xs: []float64{
				1,
				3,
				2,
				4,
			},
			want: 2.5,
		},
		{
			name: "0 percentile",
			q:    0,
			xs: []float64{
				1,
				2,
				3,
				4,
				5,
			},
			want: 1.0,
		},
		{
			name: "25 percentile",
			q:    0.25,
			xs: []float64{
				1,
				2,
				3,
				4,
				5,
			},
			want: 2.0,
		},
		{
			name: "75 percentile",
			q:    0.75,
			xs: []float64{
				1,
				2,
				3,
				4,
				5,
			},
			want: 4.0,
		},
		{
			name: "0.875 percentile",
			q:    0.875,
			xs: []float64{
				1,
				2,
				3,
				4,
				5,
			},
			want: 4.5,
		},
		{
			name: "100 percentile",
			q:    1.0,
			xs: []float64{
				1,
				2,
				3,
				4,
				5,
			},
			want: 5.0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(tt *testing.T) {
			tt.Parallel()

			got, err := Quantile(tc.xs, tc.q)
			if tc.expectErr {
				require.Error(tt, err)
				return
			}

			require.InDelta(tt, tc.want, got, 1e-6)
		})
	}
}
