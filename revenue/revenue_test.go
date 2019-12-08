package revenue

import (
	"reflect"
	"testing"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnwire"
)

// TestGetEvents tests the repeated querying of an events function to obtain
// all results from a paginated database query. It mocks the rpc call by setting
// the number of event each call  should return and checks whether all expected
// events are retrieved.
func TestGetEvents(t *testing.T) {
	// channelIDFound is a map that will successfully lookup an outpoint for
	// a zero channel ID. This blank map is used for tests where we want all
	// channels to be found.
	channelIDFound := map[lnwire.ShortChannelID]string{
		{}: "a:1",
	}

	tests := []struct {
		name string

		// queryResponses contains the number of forwarding events the test's
		// mocked query function should return on each sequential call. The
		// index of an item in this array represents the call count and the
		// value is the number of events that should be returned. For example,
		// queryResponses = [10, 2] means that the query should return 10
		// events on first call, followed by 2 events on the second call.
		// Any calls thereafter will panic.
		queryResponses []uint32

		channelMap map[lnwire.ShortChannelID]string

		// expectedEvents is the number of events we expect to be accumulated.
		expectedEvents int

		// expectedError is the error we expect to be returned.
		expectedError error
	}{
		{
			name:           "no events",
			queryResponses: []uint32{0},
			channelMap:     channelIDFound,
			expectedEvents: 0,
			expectedError:  nil,
		},
		{
			name:           "single query",
			queryResponses: []uint32{maxQueryEvents / 2},
			channelMap:     channelIDFound,
			expectedEvents: int(maxQueryEvents / 2),
			expectedError:  nil,
		},
		{
			name:           "paginated queries",
			queryResponses: []uint32{maxQueryEvents, maxQueryEvents / 2},
			channelMap:     channelIDFound,
			expectedEvents: int(maxQueryEvents) + int(maxQueryEvents)/2,
			expectedError:  nil,
		},
		{
			name:           "can't lookup channel",
			queryResponses: []uint32{maxQueryEvents / 2},
			channelMap:     make(map[lnwire.ShortChannelID]string),
			expectedEvents: 0,
			expectedError:  errUnknownChannelID,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			// Track the number of calls we have made to the mocked query function.
			callCount := 0

			query := func(offset,
				maxEvents uint32) ([]*lnrpc.ForwardingEvent, uint32, error) {

				// Get the number of forward responses the mocked function
				// should return from the test.
				count := test.queryResponses[callCount]
				callCount++

				var events []*lnrpc.ForwardingEvent

				for i := 0; uint32(i) < count; i++ {
					events = append(events, &lnrpc.ForwardingEvent{})
				}

				// Return an array with the correct number of results.
				return events, offset, nil
			}

			events, err := getEvents(test.channelMap, query)
			if err != test.expectedError {
				t.Fatalf("Expected error: %v, got: %v", test.expectedError,
					err)
			}

			// Check that we have accumulated the number of events we expect.
			if len(events) != test.expectedEvents {
				t.Fatalf("Expected %v events, got: %v",
					test.expectedEvents, len(events))
			}
		})
	}
}

// TestGetReport tests creation of a revenue report for a set of
// revenue events. It covers the case where there are no events, and
// the case where one channel is involved in multiple forwards.
func TestGetReport(t *testing.T) {
	var (
		channel1 = "a:1"
		channel2 = "a:2"
	)

	// chan1Incoming is a forwarding event where channel 1 is the incoming channel.
	chan1Incoming := revenueEvent{
		incomingChannel: channel1,
		outgoingChannel: channel2,
		incomingAmt:     1000,
		outgoingAmt:     500,
	}

	// chan1Outgoing is a forwarding event where channel1 is the outgoing channel.
	chan1Outgoing := revenueEvent{
		incomingChannel: channel2,
		outgoingChannel: channel1,
		incomingAmt:     400,
		outgoingAmt:     200,
	}

	// chan2Event is a forwarding event that channel1 is not involved in.
	chan2Event := revenueEvent{
		incomingChannel: channel2,
		outgoingChannel: channel2,
		incomingAmt:     100,
		outgoingAmt:     90,
	}

	tests := []struct {
		name           string
		events         []revenueEvent
		expectedReport *Report
	}{
		{
			name:   "no events",
			events: []revenueEvent{},
			expectedReport: &Report{
				ChannelPairs: make(map[string]map[string]Revenue),
			},
		},
		{
			name: "multiple forwards for one channel",
			events: []revenueEvent{
				chan1Incoming,
				chan1Outgoing,
				chan2Event,
			},
			expectedReport: &Report{
				ChannelPairs: map[string]map[string]Revenue{
					channel1: {
						channel2: {
							AmountOutgoing: 200,
							AmountIncoming: 1000,
							FeesOutgoing:   200,
							FeesIncoming:   500,
						},
					},
					channel2: {
						channel1: {
							AmountOutgoing: 500,
							AmountIncoming: 400,
							FeesOutgoing:   500,
							FeesIncoming:   200,
						},
						channel2: {
							AmountOutgoing: 90,
							AmountIncoming: 100,
							FeesOutgoing:   10,
							FeesIncoming:   10,
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			report := getReport(test.events)

			if !reflect.DeepEqual(report, test.expectedReport) {
				t.Fatalf("expected revenue: %v, got: %v",
					test.expectedReport, report)
			}
		})
	}
}
