package fiat

import (
	"context"
	"errors"
	"testing"
)

var errMocked = errors.New("mocked error")

// fakeQuery mocks failing and successful repeated queries, failing after a
// given call count.
type fakeQuery struct {
	// callCount tracks the number of times the httpQuery has been executed.
	callCount int

	// errorUntil is the call count at which the mock will stop failing.
	// If you do want the mock call to succeed, set this value to -1.
	// Eg, if this value is set to 2, the call will fail on first call and
	// succeed thereafter.
	errorUntil int
}

func (f *fakeQuery) call() error {
	f.callCount++

	if f.callCount <= f.errorUntil {
		return errMocked
	}

	return nil
}

// TestRetryQuery tests our retry logic, including the case where we receive
// instruction to shutdown.
func TestRetryQuery(t *testing.T) {
	tests := []struct {
		name              string
		expectedCallCount int
		expectedErr       error
		cancelContext     bool
		mock              *fakeQuery
	}{
		{
			name:              "always failing",
			expectedCallCount: maxRetries,
			expectedErr:       errRetriesFailed,
			mock: &fakeQuery{
				errorUntil: 3,
			},
		},
		{
			name:              "last call succeeds",
			expectedCallCount: 3,
			expectedErr:       nil,
			mock: &fakeQuery{
				errorUntil: 2,
			},
		},
		{
			name:              "first succeeds",
			expectedCallCount: 1,
			expectedErr:       nil,
			mock: &fakeQuery{
				errorUntil: 0,
			},
		},
		{
			name:              "call cancelled",
			expectedCallCount: 1,
			expectedErr:       errShuttingDown,
			cancelContext:     true,
			mock: &fakeQuery{
				errorUntil: 1,
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Create a test context which we can cancel.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// If we want to cancel test context to test early exit,
			// go do now.
			if test.cancelContext {
				cancel()
			}

			query := func() ([]byte, error) {
				if err := test.mock.call(); err != nil {
					return nil, err
				}

				return nil, nil
			}

			// Create a mocked parse call which acts as a nop.
			parse := func([]byte) ([]*Price, error) {
				return nil, nil
			}

			_, err := retryQuery(ctx, query, parse)
			if err != test.expectedErr {
				t.Fatalf("expected: %v, got: %v",
					test.expectedErr, err)
			}

			if test.mock.callCount != test.expectedCallCount {
				t.Fatalf("expected call count: %v, got :%v",
					test.expectedCallCount,
					test.mock.callCount)
			}
		})
	}
}
