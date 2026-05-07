package chanevents

import (
	"errors"
	"sort"

	"golang.org/x/exp/constraints"
)

// number is the type constraint Quantile accepts: any sortable numeric type.
type number interface {
	constraints.Integer | constraints.Float
}

// Quantile computes the q-quantile of a slice of comparable values. This can be
// used to compute the median (q=0.5) or the min (q=0) or max (q=1).
func Quantile[T number](xs []T, q float64) (float64, error) {
	if q < 0 || q > 1 {
		return 0, errors.New("quantile must be between 0 and 1")
	}

	if len(xs) == 0 {
		return 0, errors.New("cannot compute quantile of empty slice")
	}

	if len(xs) == 1 {
		return float64(xs[0]), nil
	}

	// Create a copy of the slice to avoid mutating the original.
	ys := make([]T, len(xs))
	copy(ys, xs)

	sort.Slice(ys, func(i, j int) bool {
		return ys[i] < ys[j]
	})

	// Compute fractional index of q-quantile.
	if q == 1.0 {
		return float64(ys[len(ys)-1]), nil
	}
	i := q * float64(len(ys)-1)

	// Interpolate between the two consecutive values, depending on the
	// fractional index position in between.
	lowerIdx := int(i)
	upperIdx := lowerIdx + 1

	lowerVal := float64(ys[lowerIdx])
	upperVal := float64(ys[upperIdx])

	indexDiff := i - float64(lowerIdx)

	return lowerVal + (upperVal-lowerVal)*indexDiff, nil
}
