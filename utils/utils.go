package utils

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

var (
	errZeroRange = errors.New("start time equals end time, not " +
		"allowed")
	errEndBeforeStart = errors.New("end time is before start time")
	errFutureRange    = errors.New("time range in future")
)

// ValidateRangeOption is an additional check that can be applied when
// validating time ranges.
type ValidateRangeOption func(startTime, endTime time.Time) error

// DisallowZeroRange is an additional check for validating time ranges which
// disallows the start time and end time to be equal.
func DisallowZeroRange(startTime, endTime time.Time) error {
	if startTime.Equal(endTime) {
		return errZeroRange
	}

	return nil
}

// DisallowFutureRange is an additional check for validating time ranges which
// disallows ranges which are in the future.
func DisallowFutureRange(startTime, endTime time.Time) error {
	now := time.Now()

	if startTime.After(now) {
		return errFutureRange
	}

	if endTime.After(now) {
		return errFutureRange
	}

	return nil
}

// ValidateTimeRange checks that a start time is before an end time. It takes
// an optional set of additional checks, and will fail if any of them error.
func ValidateTimeRange(startTime, endTime time.Time,
	checks ...ValidateRangeOption) error {

	if endTime.Before(startTime) {
		return errEndBeforeStart
	}

	for _, check := range checks {
		if err := check(startTime, endTime); err != nil {
			return err
		}
	}

	return nil
}

// GetOutPointFromString gets the channel outpoint from a string.
func GetOutPointFromString(chanStr string) (*wire.OutPoint, error) {
	chanpoint := strings.Split(chanStr, ":")
	if len(chanpoint) != 2 {
		return nil, fmt.Errorf("expected 2 parts of channel point, "+
			"got: %v", len(chanpoint))
	}

	index, err := strconv.ParseInt(chanpoint[1], 10, 32)
	if err != nil {
		return nil, err
	}

	hash, err := chainhash.NewHashFromStr(chanpoint[0])
	if err != nil {
		return nil, err
	}

	return &wire.OutPoint{
		Hash:  *hash,
		Index: uint32(index),
	}, nil
}
