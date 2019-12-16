// Package dataset provides a basic dataset type which provides functionality
// for detecting inter-quartile range outliers.
package dataset

import (
	"errors"
	"sort"
)

const (
	// weakOutlierMultiplier is the multiplier that we apply to the
	// inter-quartile range to calculate weak outliers. Using this value is
	// less cautious than using the strong outlier multiplier because it
	// will flag values that are closer to the lower/upper quartiles as
	// outliers.
	weakOutlierMultiplier = 1.5

	// strongOutlierMultiplier is the multiplier that we apply to the
	// inter-quartile range to calculate strong outliers. Using this value is
	// more cautious than using the weak outlier multiplier because it will only
	// flag extreme outliers.
	strongOutlierMultiplier = 3
)

var (
	// errNoValues is returned when an attempt is made to calculate the median of
	// a zero length array.
	errNoValues = errors.New("can't calculate median for zero length " +
		"array")

	// ErrTooFewValues is returned when there are too few values provided to
	// calculate quartiles.
	ErrTooFewValues = errors.New("can't calculate quartiles for fewer than 3 " +
		"elements")
)

// Dataset contains information about a set of float64 data points.
type Dataset map[string]float64

// getMedian gets the median for a set of *already sorted* values. It returns
// an error if there are no values.
func getMedian(values []float64) (float64, error) {
	valuesCount := len(values)
	if valuesCount == 0 {
		return 0, errNoValues
	}

	// If there is an even number of values in the dataset, return the average
	// of the values in the middle of the dataset as the median.
	if valuesCount%2 == 0 {
		return (values[(valuesCount-1)/2] + values[valuesCount/2]) / 2, nil
	}

	// If there is an odd number of values in the dataset, return the middle
	// element as the median.
	return values[valuesCount/2], nil
}

// New returns takes a map of labels to values and returns it as a dataset.
func New(valueMap map[string]float64) Dataset {
	return valueMap
}

// rawValues returns the values for a dataset without their string label. The
// values are sorted in ascending order.
func (d Dataset) rawValues() []float64 {
	values := make([]float64, 0, len(d))
	for _, value := range d {
		values = append(values, value)
	}

	// Sort the dataset in ascending order.
	sort.Float64s(values)
	return values
}

// quartiles returns the upper and lower quartiles of a dataset. It will fail if
// there are fewer than 3 values in the dataset, because we cannot calculate
// quartiles for fewer than 3 values.
func (d Dataset) quartiles() (float64, float64, error) {
	valueCount := len(d)
	if valueCount < 3 {
		return 0, 0, ErrTooFewValues
	}

	// Get the cutoff points for calculating the lower and upper quartiles.
	// The "exclusive" method of calculating quartiles is used, meaning that
	// the dataset is split in half, excluding the median value in the case
	// of an odd number of elements.
	var cutoffLower, cutoffUpper int
	if valueCount%2 == 0 {
		// For an even number of elements, we split the dataset exactly in half.
		cutoffLower = valueCount / 2
		cutoffUpper = valueCount / 2
	} else {
		// For an odd number of elements, we exclude the middle element by
		// returning cutoff points on either side of it.
		cutoffLower = (valueCount - 1) / 2
		cutoffUpper = cutoffLower + 1
	}

	rawValues := d.rawValues()
	lowerQuartile, err := getMedian(rawValues[:cutoffLower])
	if err != nil {
		return 0, 0, err
	}

	upperQuartile, err := getMedian(rawValues[cutoffUpper:])
	if err != nil {
		return 0, 0, err
	}

	return lowerQuartile, upperQuartile, nil
}

// OutlierResult returns the results of an outlier check.
type OutlierResult struct {
	// UpperOutlier is true if the value is an upper outlier in the dataset.
	UpperOutlier bool

	// LowerOutlier is true if the value is a lower outlier in the dataset.
	LowerOutlier bool
}

// isIQROutlier returns an outlier result which indicates whether a value is an
// upper or lower outlier (or not an outlier) for the dataset.
// If a value is an upper or lower outlier, the result is recorded in the
// corresponding bool in an outlier result.
func (d Dataset) isIQROutlier(value float64, lowerQuartile,
	upperQuartile float64, strong bool) *OutlierResult {

	interquartileRange := upperQuartile - lowerQuartile

	// quartileDistance is the distance from the upper/lower quartile a value
	// must be to be considered an outlier. A larger quartile distance more
	// strictly classifies outliers, because they are required to be further
	// from the upper/lower quartile. Based on whether we want to find strong or
	// weak outliers, the inter-quartile range (which is the base unit for this
	// distance) is multiplier by a strong or weak multiplier.
	quartileDistance := interquartileRange * weakOutlierMultiplier
	if strong {
		quartileDistance = interquartileRange * strongOutlierMultiplier
	}

	return &OutlierResult{
		// A value is considered to be a upper outlier if it lies above the
		// upper quartile by the chosen quartile distance.
		UpperOutlier: value > upperQuartile+quartileDistance,

		// A value is considered to be a lower outlier if it lies beneath the
		// lower quartile by the chosen quartile distance for calculating
		// outliers.
		LowerOutlier: value < lowerQuartile-quartileDistance,
	}
}

// GetOutliers returns a map of the labels in the dataset to outlier results
// which indicate whether the associated value is an upper or lower inter-
// quartile outlier.
//
// Strong is set to adjust whether we check for a strong or weak outlier. Strong
// outliers are 3 inter-quartile ranges below/above the lower/upper quartile and
// weak outliers are 1.5 inter-quartile ranges below/above the lower/upper
// quartile.
//
// Given some random set of data, with lower quartile = 5 and upper quartile
// = 6, the inter-quartile range is 1.
//
//         LQ             UQ
// [ 1  2  5  5  5  6  6  6  8 11 ]
//
// For strong outliers, we multiply the inter-quartile range by 3 then check
// whether a value is below the lower quartile or above the upper quartile by
// that amount to determine whether it is a lower or upper outlier.
// Strong lower outlier bound: 5 - (1 * 3) = 2
//   -> 1 is a strong lower outlier
// Strong upper outlier bound: 6 + (1 * 3) = 9
//   -> 11 is a strong upper outlier
//
// For weak outliers, we perform the same check, but we multiply the inter-
// quartile range by 1.5 rather than 3.
// Weak lower outlier bound: 5 - (1 * 1.5) = 3.5
//   -> 1 and 2 are weak lower outliers
// Weak upper outlier bound: 6 + (1 *1.5) = 7.5
//   -> 8 and 11 are weak upper outliers
func (d Dataset) GetOutliers(strong bool) (map[string]*OutlierResult, error) {
	lower, upper, err := d.quartiles()
	if err != nil {
		return nil, err
	}

	outliers := make(map[string]*OutlierResult, len(d))

	for label, value := range d {
		outliers[label] = d.isIQROutlier(value, lower, upper, strong)
	}

	return outliers, nil
}
