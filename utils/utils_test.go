package utils

import (
	"crypto/rand"
	"reflect"
	"testing"
	"time"

	"github.com/btcsuite/btcd/wire"
)

// TestValidateTimeRange tests validation of time ranges and optional checks
// that can be added.
func TestValidateTimeRange(t *testing.T) {
	now := time.Now()
	hourAgo := now.Add(time.Hour * -1)
	future := now.Add(time.Hour)

	tests := []struct {
		name        string
		startTime   time.Time
		endTime     time.Time
		opts        []ValidateRangeOption
		expectedErr error
	}{
		{
			name:        "start before end",
			startTime:   hourAgo,
			endTime:     now,
			expectedErr: nil,
		},
		{
			// We allow equal ranges when we do not have an
			// additional check in place.
			name:        "start equals end",
			startTime:   hourAgo,
			endTime:     hourAgo,
			expectedErr: nil,
		},
		{
			name:      "start equals end disallowed",
			startTime: hourAgo,
			endTime:   hourAgo,
			opts: []ValidateRangeOption{
				DisallowZeroRange,
			},
			expectedErr: errZeroRange,
		},
		{
			name:        "end before start",
			startTime:   now,
			endTime:     hourAgo,
			expectedErr: errEndBeforeStart,
		},
		{
			// Range in future is ok when we don't have another
			// check.
			name:        "range in future",
			startTime:   now,
			endTime:     future,
			expectedErr: nil,
		},
		{
			name:      "range in future disallowed",
			startTime: now,
			endTime:   future,
			opts: []ValidateRangeOption{
				DisallowFutureRange,
			},
			expectedErr: errFutureRange,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateTimeRange(
				test.startTime, test.endTime, test.opts...,
			)
			if err != test.expectedErr {
				t.Fatalf("expected %v, got: %v",
					test.expectedErr, err)
			}
		})
	}
}

// TestGetOutPointFromString tests parsing of an outpoint from a string.
func TestGetOutPointFromString(t *testing.T) {
	var txid [32]byte

	if _, err := rand.Read(txid[:]); err != nil {
		t.Fatalf("cannot generate txid: %v", err)
	}

	exampleOutpoint := &wire.OutPoint{
		Hash:  txid,
		Index: 1,
	}

	tests := []struct {
		name             string
		value            string
		expectedOutpoint *wire.OutPoint
		expectError      bool
	}{
		{
			name:        "no separator",
			value:       "example",
			expectError: true,
		},
		{
			name:        "too many separated values",
			value:       "a:b:c",
			expectError: true,
		},
		{
			name:        "non-numerical outpoint",
			value:       "a:b",
			expectError: true,
		},
		{
			name:             "ok",
			value:            exampleOutpoint.String(),
			expectError:      false,
			expectedOutpoint: exampleOutpoint,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			outPoint, err := GetOutPointFromString(test.value)
			gotError := err != nil
			if gotError != test.expectError {
				t.Fatalf("expected: %v, got: %v", test.expectError, gotError)
			}

			// If we expect an error, we do not need to validate the outpoint returned.
			if test.expectError {
				return
			}

			if !reflect.DeepEqual(outPoint, test.expectedOutpoint) {
				t.Fatalf("expected: %v, got: %v", test.expectedOutpoint, outPoint)
			}
		})
	}
}
