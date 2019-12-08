package revenue

import (
	"reflect"
	"testing"
)

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
