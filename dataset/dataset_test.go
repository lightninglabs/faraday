package dataset

import (
	"fmt"
	"testing"
)

// TestGetMedian tests median calculation for a series of inputs, including
// the error case where there are no values.
func TestGetMedian(t *testing.T) {
	tests := []struct {
		name           string
		values         []float64
		expectedErr    error
		expectedMedian float64
	}{
		{
			name:        "no values",
			values:      []float64{},
			expectedErr: errNoValues,
		},
		{
			name:           "one value",
			values:         []float64{1},
			expectedErr:    nil,
			expectedMedian: 1,
		},
		{
			name:           "two values",
			values:         []float64{1, 2},
			expectedErr:    nil,
			expectedMedian: 1.5,
		},
		{
			name:           "three values",
			values:         []float64{1, 2, 3},
			expectedErr:    nil,
			expectedMedian: 2,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			median, err := getMedian(test.values)
			if err != test.expectedErr {
				t.Fatalf("expected: %v, got: %v", test.expectedErr, err)
			}

			if test.expectedMedian != median {
				t.Fatalf("expected: %v, got: %v", test.expectedMedian, median)
			}
		})
	}
}

// TestQuartiles tests getting of upper and lower quartiles for a dataset. It
// tests the case where the dataset does not have enough values, and cases with
// odd and even numbers of values to test the splitting of the dataset.
func TestQuartiles(t *testing.T) {
	tests := []struct {
		name                  string
		values                []float64
		expectedErr           error
		expectedLowerQuartile float64
		expectedUpperQuartile float64
	}{
		{
			name:        "no elements",
			values:      []float64{},
			expectedErr: ErrTooFewValues,
		},
		{
			name:                  "three elements",
			values:                []float64{3, 1, 2},
			expectedLowerQuartile: 1,
			expectedUpperQuartile: 3,
		},
		{
			name:                  "four elements",
			values:                []float64{1, 2, 3, 4},
			expectedLowerQuartile: 1.5,
			expectedUpperQuartile: 3.5,
		},
		{
			name:                  "five elements",
			values:                []float64{1, 2, 4, 3, 5},
			expectedLowerQuartile: 1.5,
			expectedUpperQuartile: 4.5,
		},
		{
			name:                  "eight elements",
			values:                []float64{1, 2, 3, 4, 5, 6, 7, 8},
			expectedLowerQuartile: 2.5,
			expectedUpperQuartile: 6.5,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Create a map of dummy outpoints to values to create the dataset
			// so that each test case does not need to create maps.
			valueMap := make(map[string]float64)
			for i, value := range test.values {
				valueMap[fmt.Sprintf("%v", i)] = value
			}

			dataset := New(valueMap)

			lower, upper, err := dataset.quartiles()
			if err != test.expectedErr {
				t.Fatalf("expected: %v, got: %v", test.expectedErr, err)
			}

			// If an error occurred, we do not need to perform any further
			// checks.
			if err != nil {
				return
			}

			if test.expectedLowerQuartile != lower {
				t.Fatalf("expected: %v, got: %v",
					test.expectedLowerQuartile, lower)
			}

			if test.expectedUpperQuartile != upper {
				t.Fatalf("expected: %v, got: %v",
					test.expectedUpperQuartile, upper)
			}
		})
	}
}

// TestIsOutlier tests getting of upper and lower interquartile outliers.
func TestIsOutlier(t *testing.T) {
	// noOutlier is a outlier result for a value which is not an outlier.
	noOutlier := &OutlierResult{}

	tests := []struct {
		name             string
		values           map[string]float64
		expectedError    error
		expectedOutliers map[string]*OutlierResult
		multiplier       float64
	}{
		{
			name:          "too few values",
			expectedError: ErrTooFewValues,
			values:        make(map[string]float64),
			multiplier:    3,
		},
		{
			name: "lower outlier",
			values: map[string]float64{
				"a": 1,
				"b": 7,
				"c": 7,
				"d": 8,
				"e": 8,
				"f": 10,
			},
			multiplier: 3,
			expectedOutliers: map[string]*OutlierResult{
				"a": {
					UpperOutlier: false,
					LowerOutlier: true,
				},
				"b": noOutlier,
				"c": noOutlier,
				"d": noOutlier,
				"e": noOutlier,
				"f": noOutlier,
			},
		},
		{
			name: "upper outlier",
			values: map[string]float64{
				"a": 1,
				"b": 1,
				"c": 2,
				"d": 2,
				"e": 3,
				"f": 10,
			},
			multiplier: 3,
			expectedOutliers: map[string]*OutlierResult{
				"a": noOutlier,
				"b": noOutlier,
				"c": noOutlier,
				"d": noOutlier,
				"e": noOutlier,
				"f": {
					UpperOutlier: true,
					LowerOutlier: false,
				},
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			dataset := New(test.values)

			outliers, err := dataset.GetOutliers(test.multiplier)
			if err != test.expectedError {
				t.Fatalf("expected: %v, got: %v", test.expectedError, err)
			}

			// If the error is non-nil, there is no need for further checks.
			if err != nil {
				return
			}

			for label, outlier := range outliers {
				expectedOutlier, ok := test.expectedOutliers[label]
				if !ok {
					t.Fatalf("outlier label: %v not expected", label)
				}

				if outlier.LowerOutlier != expectedOutlier.LowerOutlier {
					t.Fatalf("expected lower outlier: %v, got: %v for: %v",
						expectedOutlier.LowerOutlier, outlier.LowerOutlier,
						label)
				}
				if outlier.UpperOutlier != expectedOutlier.UpperOutlier {
					t.Fatalf("expected upper outlier: %v, got: %v for: %v",
						expectedOutlier.UpperOutlier, outlier.UpperOutlier,
						label)
				}
			}
		})
	}
}
