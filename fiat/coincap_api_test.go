package fiat

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestCoinCapGetPrices tests splitting of a query period into the number of
// requests required to obtain the desired granularity.
func TestCoinCapGetPrices(t *testing.T) {
	now := time.Now()
	halfDayAgo := now.Add(time.Hour * -12)
	twoDayAgo := now.Add(time.Hour * -24 * 2)
	fiveDaysAgo := now.Add(time.Hour * -24 * 5)
	tenDaysAgo := now.Add(time.Hour * -24 * 10)

	tests := []struct {
		name              string
		granularity       Granularity
		startTime         time.Time
		endTime           time.Time
		mock              *fakeQuery
		expectedCallCount int
		expectedErr       error
	}{
		{
			name:              "unknown granularity",
			granularity:       Granularity("unknown"),
			mock:              &fakeQuery{},
			expectedCallCount: 0,
			expectedErr:       errUnknownGranularity,
		},
		{
			name:              "range below minimum",
			granularity:       GranularityMinute,
			startTime:         now,
			endTime:           now,
			mock:              &fakeQuery{},
			expectedCallCount: 1,
			expectedErr:       nil,
		},
		{
			// One minute has a maximum 24H query period, ten days
			// will be too long.
			name:              "10 queries - exceeded",
			granularity:       GranularityMinute,
			startTime:         tenDaysAgo,
			endTime:           now,
			mock:              &fakeQuery{},
			expectedCallCount: 0,
			expectedErr:       errPeriodTooLong,
		},
		{
			// One minute has a maximum 24H period, five days
			// should be exactly fine.
			name:              "5 queries - ok",
			granularity:       GranularityMinute,
			startTime:         fiveDaysAgo,
			endTime:           now,
			mock:              &fakeQuery{},
			expectedCallCount: 5,
			expectedErr:       nil,
		},
		{
			// One minute has a maximum 24H period, two days should
			// only require two queries.
			name:              "2 queries - ok",
			granularity:       GranularityMinute,
			startTime:         twoDayAgo,
			endTime:           now,
			mock:              &fakeQuery{},
			expectedCallCount: 2,
			expectedErr:       nil,
		},
		{
			// One minute has a maximum 24H period, half a day
			// ago should only require one query.
			name:              "1 query - ok",
			granularity:       GranularityMinute,
			startTime:         halfDayAgo,
			endTime:           now,
			mock:              &fakeQuery{},
			expectedCallCount: 1,
			expectedErr:       nil,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			// Create a mocked query function which will track
			// our call count and error as required for the test.
			query := func(_, _ time.Time,
				_ Granularity) ([]byte, error) {

				if err := test.mock.call(); err != nil {
					return nil, err
				}

				return nil, nil
			}

			// Create a mocked convert function.
			convert := func([]byte) ([]*USDPrice, error) {
				return nil, nil
			}

			coinCapAPI := coinCapAPI{
				granularity: test.granularity,
				query:       query,
				convert:     convert,
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			_, err := coinCapAPI.GetPrices(
				ctx, test.startTime, test.endTime,
			)
			if err != test.expectedErr {
				t.Fatalf("expected: %v,got: %v",
					test.expectedErr, err)
			}

			if test.expectedCallCount != test.mock.callCount {
				t.Fatalf("expected call count: %v, got: %v",
					test.expectedCallCount,
					test.mock.callCount)
			}
		})
	}
}

// TestBestGranularity tests getting of the lowest granularity possible for
// a given query duration.
func TestBestGranularity(t *testing.T) {
	period1Min, _ := GranularityMinute.maxSplitDuration()
	period15Min, _ := Granularity15Minute.maxSplitDuration()
	periodMax, _ := GranularityDay.maxSplitDuration()

	tests := []struct {
		name        string
		duration    time.Duration
		granularity Granularity
		err         error
	}{
		{
			name:        "equal to interval max",
			duration:    period1Min,
			granularity: GranularityMinute,
			err:         nil,
		},
		{
			name:        "less than interval",
			duration:    time.Second,
			granularity: GranularityMinute,
			err:         nil,
		},
		{
			name:        "multiple queries to m15",
			duration:    period15Min - 100,
			granularity: Granularity15Minute,
			err:         nil,
		},
		{
			name:        "too long",
			duration:    periodMax + 1,
			granularity: "",
			err:         ErrQueryTooLong,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			best, err := BestGranularity(test.duration)
			require.Equal(t, test.err, err)
			require.Equal(t, test.granularity, best)
		})
	}
}
