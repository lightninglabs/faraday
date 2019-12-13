package utils

import (
	"crypto/rand"
	"reflect"
	"testing"

	"github.com/btcsuite/btcd/wire"
)

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
