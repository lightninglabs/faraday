package revenue

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnwire"
)

// TestGetRevenueReport tests querying for a revenue report.
func TestGetRevenueReport(t *testing.T) {
	var (
		// testErr is an error returned by the mock to simulate rpc failures.
		testErr = errors.New("error thrown by mock")

		chan1 = &lnrpc.Channel{
			ChannelPoint: "a:1",
			ChanId:       123,
		}

		chan2 = &lnrpc.Channel{
			ChannelPoint: "a:2",
			ChanId:       321,
		}
	)

	tests := []struct {
		name           string
		listChanErr    error
		closedChanErr  error
		forwardHistErr error
		openChannels   []*lnrpc.Channel
		closedChannels []*lnrpc.ChannelCloseSummary
		fwdHistory     []*lnrpc.ForwardingEvent
		expectedReport *Report
		expectErr      error
	}{
		{
			name:        "open channels fails",
			listChanErr: testErr,
			expectErr:   testErr,
		},
		{
			name:          "closed channels fails",
			closedChanErr: testErr,
			expectErr:     testErr,
		},
		{
			name:           "forward history fails",
			forwardHistErr: testErr,
			expectErr:      testErr,
		},
		{
			name: "cannot find channel",
			fwdHistory: []*lnrpc.ForwardingEvent{
				{
					ChanIdIn: 123,
				},
			},
			expectErr: nil,
			expectedReport: &Report{
				ChannelPairs: make(map[string]map[string]Revenue),
			},
		},
		{
			name:         "open and closed channel",
			openChannels: []*lnrpc.Channel{chan1},
			closedChannels: []*lnrpc.ChannelCloseSummary{{
				ChannelPoint: chan2.ChannelPoint,
				ChanId:       chan2.ChanId,
			}},
			fwdHistory: []*lnrpc.ForwardingEvent{
				{
					ChanIdIn:   chan1.ChanId,
					ChanIdOut:  chan2.ChanId,
					AmtOutMsat: 100,
					AmtInMsat:  150,
				},
			},
			expectedReport: &Report{
				ChannelPairs: map[string]map[string]Revenue{
					chan1.ChannelPoint: {
						chan2.ChannelPoint: Revenue{
							AmountIncoming: 150,
							AmountOutgoing: 0,
							FeesIncoming:   50,
							FeesOutgoing:   0,
						}},
					chan2.ChannelPoint: {
						chan1.ChannelPoint: Revenue{
							AmountIncoming: 0,
							AmountOutgoing: 100,
							FeesIncoming:   0,
							FeesOutgoing:   50,
						}},
				}},
			expectErr: nil,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			// Create a config which returns the tests's specified responses
			// and errors.
			cfg := &Config{
				ListChannels: func() ([]*lnrpc.Channel, error) {
					return test.openChannels, test.listChanErr
				},
				ClosedChannels: func() ([]*lnrpc.ChannelCloseSummary, error) {
					return test.closedChannels, test.closedChanErr
				},
				ForwardingHistory: func(startTime, endTime time.Time, offset,
					max uint32) ([]*lnrpc.ForwardingEvent, uint32, error) {

					return test.fwdHistory, offset, test.forwardHistErr
				},
			}

			report, err := GetRevenueReport(
				cfg, time.Now(), time.Now(),
			)
			if test.expectErr != err {
				t.Fatalf("expected: %v, got: %v", test.expectErr, err)
			}

			if !reflect.DeepEqual(test.expectedReport, report) {
				t.Fatalf("expected: \n%+v, got: \n%+v", test.expectedReport, report)
			}

		})
	}
}

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
	}{
		{
			name:           "no events",
			queryResponses: []uint32{0},
			channelMap:     channelIDFound,
			expectedEvents: 0,
		},
		{
			name:           "single query",
			queryResponses: []uint32{maxQueryEvents / 2},
			channelMap:     channelIDFound,
			expectedEvents: int(maxQueryEvents / 2),
		},
		{
			name:           "paginated queries",
			queryResponses: []uint32{maxQueryEvents, maxQueryEvents / 2},
			channelMap:     channelIDFound,
			expectedEvents: int(maxQueryEvents) + int(maxQueryEvents)/2,
		},
		{
			name:           "can't lookup channel skips event",
			queryResponses: []uint32{maxQueryEvents / 2},
			channelMap:     make(map[lnwire.ShortChannelID]string),
			expectedEvents: 0,
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
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
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
