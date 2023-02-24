// Package dataset provides a basic dataset type which provides functionality
// for detecting inter-quartile range outliers.
package dataset

import (
	"errors"
	"sort"
)

var (
	// errNoValues is returned when an attempt is made to calculate the
	// median of a zero length array.
	errNoValues = errors.New("can't calculate median for zero length " +
		"array")

	// errTooFewValues is returned when there are too few values provided
	// to calculate quartiles.
	errTooFewValues = errors.New("can't calculate quartiles for fewer" +
		" than 3 elements")
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

	// If there is an even number of values in the dataset, return the
	// average of the values in the middle of the dataset as the median.
	if valuesCount%2 == 0 {
		return (values[(valuesCount-1)/2] + values[valuesCount/2]) / 2, nil
	}

	// If there is an odd number of values in the dataset, return the
	// middle element as the median.
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

// quartiles returns the upper and lower quartiles of a dataset. It will fail
// if there are fewer than 3 values in the dataset, because we cannot calculate
// quartiles for fewer than 3 values.
func (d Dataset) quartiles() (float64, float64, error) {
	valueCount := len(d)
	if valueCount < 3 {
		return 0, 0, errTooFewValues
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

// Value returns the value that a label is associated with in a set.
func (d Dataset) Value(label string) float64 {
	return d[label]
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
func (d Dataset) isIQROutlier(value, lowerQuartile, upperQuartile,
	multiplier float64) *OutlierResult {

	interquartileRange := upperQuartile - lowerQuartile

	// quartileDistance is the distance from the upper/lower quartile a value
	// must be to be considered an outlier. A larger quartile distance more
	// strictly classifies outliers, because they are required to be further
	// from the upper/lower quartile.
	quartileDistance := interquartileRange * multiplier

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
// quartile outlier. If there are too few values to calculate inter-quartile
// outliers, it will return false values for all data points.
//
// An outlier multiplier is provided to determine how strictly we classify
// outliers; lower values will identify more outliers, thus being more strict,
// and higher values will identify fewer outliers, thus being less strict.
// Multipliers less than 1.5 are considered to provide "weak outliers", because
// the values are still relatively close the the rest of the dataset.
// Multipliers more than 3 are considered to provide "strong outliers" because
// they identify values that are far from the rest of the dataset.
//
// The effect of this value is illustrated in the example below:
// Given some random set of data, with lower quartile = 5 and upper quartile
// = 6, the inter-quartile range is 1.
//
//	        LQ             UQ
//	[ 1  2  5  5  5  6  6  6  8 11 ]
//
// For larger values, eg multiplier=3, we will detect fewer outliers:
// Lower outlier bound: 5 - (1 * 3) = 2
//
//	-> 1 is a strong lower outlier
//
// Upper outlier bound: 6 + (1 * 3) = 9
//
//	-> 11 is a strong upper outlier
//
// For smaller values, eg multiplier=1.5, we detect more outliers:
// Weak lower outlier bound: 5 - (1 * 1.5) = 3.5
//
//	-> 1 and 2 are weak lower outliers
//
// Weak upper outlier bound: 6 + (1 *1.5) = 7.5
//
//	-> 8 and 11 are weak upper outliers
func (d Dataset) GetOutliers(outlierMultiplier float64) (
	map[string]*OutlierResult, error) {

	outliers := make(map[string]*OutlierResult, len(d))

	lower, upper, err := d.quartiles()
	// If we could not calculate quartiles because there are too few values,
	// we cannot calculate outliers so we return a map with all false
	// outlier results.
	if err == errTooFewValues {
		log.Debug("could not calculate quartiles: %v, returning an "+
			"empty set of outliers", err)

		// Return a map with no outliers.
		for label := range d {
			outliers[label] = &OutlierResult{
				UpperOutlier: false,
				LowerOutlier: false,
			}
		}
		return outliers, nil
	}
	if err != nil {
		return nil, err
	}

	log.Tracef("quartiles calculated for: %v items: upper quartile: %v, "+
		"lower quartile: %v", len(d), upper, lower)

	// If we could could calculate quartiles for the dataset, we get
	// outliers and populate a result map.
	for label, value := range d {
		outliers[label] = d.isIQROutlier(
			value, lower, upper, outlierMultiplier,
		)
	}

	return outliers, nil
}

// GetThreshold returns the set of values in a dataset <= or > a given
// threshold. The below bool is used to toggle whether we identify values
// above or below the threshold.
func (d Dataset) GetThreshold(thresholdValue float64, below bool) map[string]bool {
	threshold := make(map[string]bool, len(d.rawValues()))

	log.Tracef("examining %v items with threshold: %v, looking "+
		"for <= threshold: %v", len(d), thresholdValue, below)

	for label, value := range d {
		// If we are looking for values below the threshold, check the
		// current value then move on to the next one.
		if below {
			// If the value is below or equal to the threshold, we
			// set the label's value in the map to true. Otherwise
			// we set it to false.
			threshold[label] = value <= thresholdValue
			continue
		}

		// We are looking for values above the threshold. Set the
		// label's value to true if the value is greater than the
		// threshold.
		threshold[label] = value > thresholdValue
	}

	return threshold
}
