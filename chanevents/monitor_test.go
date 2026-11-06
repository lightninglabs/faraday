package chanevents

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/clock"
	"github.com/stretchr/testify/require"
)

// TestNextReconnectDelay pins the reconnect backoff. Each reconnect writes a
// full sync batch, so a subscription that fails at once must back off to the
// cap, and a subscription that handled an event must reset the wait.
func TestNextReconnectDelay(t *testing.T) {
	t.Parallel()

	var delay time.Duration
	var got []time.Duration
	for range 8 {
		delay = nextReconnectDelay(delay, false)
		got = append(got, delay)
	}
	require.Equal(t, []time.Duration{
		5 * time.Second, 10 * time.Second, 20 * time.Second,
		40 * time.Second, 80 * time.Second, 160 * time.Second,
		maxReconnectInterval, maxReconnectInterval,
	}, got)

	require.Equal(t, retryInterval, nextReconnectDelay(delay, true))
}

// syncLnd is an lnd stub that scripts the result of each initial sync and
// records a subscription that does not follow a successful sync. The embedded
// interface is nil, so any method it does not override panics.
type syncLnd struct {
	lndclient.LightningClient

	// syncErrs holds the result of each initial sync. done closes once
	// the monitor attempted all of them.
	syncErrs []error
	done     chan struct{}

	mu       sync.Mutex
	syncs    int
	synced   bool
	unsynced bool
}

func (l *syncLnd) GetInfo(_ context.Context) (*lndclient.Info, error) {
	return &lndclient.Info{SyncedToChain: true}, nil
}

func (l *syncLnd) ClosedChannels(_ context.Context) (
	[]lndclient.ClosedChannel, error) {

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.syncs == len(l.syncErrs) {
		return nil, errors.New("sync not scripted")
	}

	err := l.syncErrs[l.syncs]
	l.syncs++
	if l.syncs == len(l.syncErrs) {
		close(l.done)
	}

	return nil, err
}

// ListChannels is the last lnd call of a successful initial sync.
func (l *syncLnd) ListChannels(_ context.Context, _, _ bool,
	_ ...lndclient.ListChannelsOption) ([]lndclient.ChannelInfo, error) {

	l.mu.Lock()
	defer l.mu.Unlock()

	l.synced = true

	return nil, nil
}

func (l *syncLnd) SubscribeChannelEvents(_ context.Context) (
	<-chan *lndclient.ChannelEventUpdate, <-chan error, error) {

	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.synced {
		l.unsynced = true
	}
	l.synced = false

	return nil, nil, errors.New("subscription unavailable")
}

// TestMonitorSyncsBeforeSubscribe pins that the monitor writes a sync batch
// before each subscription. Without it, a live event would end the unobserved
// interval, and the outage would count as observed time. Each case waits one
// reconnect interval.
func TestMonitorSyncsBeforeSubscribe(t *testing.T) {
	t.Parallel()

	syncErr := errors.New("closed channels unavailable")
	tests := []struct {
		name     string
		syncErrs []error
	}{
		{"failed sync", []error{syncErr, syncErr}},
		{"reconnect", []error{nil, nil}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lnd := &syncLnd{
				syncErrs: tc.syncErrs,
				done:     make(chan struct{}),
			}
			store := NewTestDB(t, clock.NewTestClock(testTime))
			monitor := NewMonitor(lnd, store, Config{})
			require.NoError(t, monitor.Start(t.Context()))

			select {
			case <-lnd.done:
			case <-time.After(30 * time.Second):
				t.Fatal("initial sync not retried")
			}
			require.NoError(t, monitor.Stop())

			lnd.mu.Lock()
			defer lnd.mu.Unlock()
			require.False(t, lnd.unsynced)
		})
	}
}
