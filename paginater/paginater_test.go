package paginater

import (
	"context"
	"fmt"
	"testing"
)

// mockPaginatedAPI mocks querying a paginated api. It takes a cancel function
// and index to allow us to mock cancellation of client context on a given
// call count.
type mockPaginatedAPI struct {
	// callCount is the number of calls we have made.
	callCount int

	// callResponses contains the number of items the test's mocked query
	// function should return on each sequential call. The index of an item
	// in this array represents the call count and the value is the number
	// of events that should be returned. For example, [10, 2] means that
	// the query should return 10 events on first call, followed by 2
	// events on the second call. Any calls thereafter will error.
	callResponses []int

	// cancelIndex is the call index after which we want to can
	cancelIndex int

	// cancel is used to mock client cancellation after a given call count.
	cancel func()
}

func (m *mockPaginatedAPI) query(_, _ uint64) (uint64, uint64, error) {
	if len(m.callResponses) == m.callCount {
		return 0, 0, fmt.Errorf("mock not configured with enough calls")
	}

	// If we have reached the index at which we would like
	// to cancel our context, we do so before making our
	// call.
	if m.cancelIndex == m.callCount {
		m.cancel()
	}

	// Get the number of events we would like our mock to return from its
	// list of call counts.
	numEvents := m.callResponses[m.callCount]

	// Increment our internal call count.
	m.callCount++

	// Return a 0 offset (we don't need it for tests) and the number of
	// events we require.
	return 0, uint64(numEvents), nil
}

// newMockPaginater creates a new mock with the call counts provided.
func newMockPaginater(callCounts []int, cancelIndex int,
	cancel func()) *mockPaginatedAPI {

	return &mockPaginatedAPI{
		callCount:     0,
		cancelIndex:   cancelIndex,
		cancel:        cancel,
		callResponses: callCounts,
	}
}

// TestGetEvents tests the repeated querying of an events function to obtain
// all results from a paginated database query. It mocks the rpc call by setting
// the number of event each call  should return and checks whether all expected
// events are retrieved.
func TestGetEvents(t *testing.T) {
	var maxQueryEvents = 50

	tests := []struct {
		name string

		// returnCount is the number of events we expect each call to
		// return.
		returnCount []int

		// expectedCalls is the number of calls we expect the mock to
		// make.
		expectedCalls int

		// cancelIndex is the call count after which we want to cancel
		// our context. We use this to mock client side cancels. This
		// value should be set to -1 if we don't want to cancel ever.
		cancelIndex int
	}{
		{
			name:          "no events",
			returnCount:   []int{0},
			expectedCalls: 1,
			cancelIndex:   -1,
		},
		{
			name:          "single query",
			returnCount:   []int{maxQueryEvents - 1},
			expectedCalls: 1,
			cancelIndex:   -1,
		},
		{
			name: "paginated queries",
			returnCount: []int{
				maxQueryEvents, maxQueryEvents - 1,
			},
			expectedCalls: 2,
			cancelIndex:   -1,
		},
		{
			// We set our context to cancel at call index 1, so we
			// expect our loop to exit on our second call despite
			// having enough events for 3 queries.
			name: "paginated queries, cancelled",
			returnCount: []int{
				maxQueryEvents, maxQueryEvents, maxQueryEvents,
			},
			expectedCalls: 2,
			cancelIndex:   1,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			mock := newMockPaginater(
				test.returnCount, test.cancelIndex, cancel,
			)

			err := QueryPaginated(
				ctx, mock.query, 0,
				uint64(maxQueryEvents),
			)
			// Check our error with the exception of a context err
			// which will be returned if we are cancelling our
			// context as part of the test.
			if err != nil && err != ctx.Err() {
				t.Fatalf("unexpected error: %v", err)
			}

			if mock.callCount != test.expectedCalls {
				t.Fatalf("expected: %v calls, got: %v",
					test.expectedCalls, mock.callCount)
			}
		})
	}
}
