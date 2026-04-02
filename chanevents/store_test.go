package chanevents

import (
	"context"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightningnetwork/lnd/clock"
	"github.com/lightningnetwork/lnd/fn/v2"
	"github.com/stretchr/testify/require"
)

var (
	testPubKey = "028d4c6347426f2e3f5e2b8e4a1c3b9f1" +
		"c4e5d6f7a8b9c0d1e2f3a4b5c6d7e8f9"
	testChanPoint1          = "test_txid:0"
	testChanPoint2          = "test_txid:1"
	testShortChanID1 uint64 = 123
	testShortChanID2 uint64 = 456

	testTime = time.Unix(1, 0)
)

// requireEqualEvent asserts that a retrieved event matches the expected values,
// comparing only the fields that are set before insertion (ignoring the
// auto-assigned ID).
func requireEqualEvent(t *testing.T, expected *ChannelEvent,
	expectedTime time.Time, actual *ChannelEvent) {

	t.Helper()

	require.Equal(t, expected.ChannelID, actual.ChannelID)
	require.Equal(t, expected.EventType, actual.EventType)
	require.Equal(t, expectedTime.Unix(), actual.Timestamp.Unix())
	require.Equal(t, expected.LocalBalance, actual.LocalBalance)
	require.Equal(t, expected.RemoteBalance, actual.RemoteBalance)
	require.Equal(t, expected.IsSync, actual.IsSync)
}

// TestStore tests the chanevents store.
func TestStore(t *testing.T) {
	t.Parallel()

	// First, create a new test database.
	clock := clock.NewTestClock(testTime)
	store := NewTestDB(t, clock)
	ctx := context.Background()

	// *** Peers *** Add a peer.
	peer := &Peer{PubKey: testPubKey}
	peerID, err := store.AddPeer(ctx, peer.PubKey)
	require.NoError(t, err)
	require.NotZero(t, peerID)

	// Adding the same peer again violates the unique constraint.
	_, err = store.AddPeer(ctx, peer.PubKey)
	require.Error(t, err)

	dbPeer, err := store.GetPeer(ctx, "non_existent_pubkey")
	require.ErrorIs(t, err, errUnknownPeer)
	require.Nil(t, dbPeer)

	// Get the peer and assert it is the same.
	dbPeer, err = store.GetPeer(ctx, peer.PubKey)
	require.NoError(t, err)
	require.Equal(t, peer.PubKey, dbPeer.PubKey)

	// *** Channels *** Add a channel for an unknown peer and assert an
	// error is returned.
	channelID, err := store.AddChannel(
		ctx, testChanPoint1, testShortChanID1, 9999,
	)
	require.Error(t, err)
	require.Zero(t, channelID)

	// Add a channel for the peer.
	channelID, err = store.AddChannel(
		ctx, testChanPoint1, testShortChanID1, peerID,
	)
	require.NoError(t, err)
	require.NotZero(t, channelID)

	// Get a non-existent channel and assert an error is returned.
	dbChannel, err := store.GetChannel(ctx, "non-existent-chan-point")
	require.ErrorIs(t, err, errUnknownChannel)
	require.Nil(t, dbChannel)

	// Get the channel and assert it is the same.
	dbChannel, err = store.GetChannel(ctx, testChanPoint1)
	require.NoError(t, err)
	require.Equal(t, testChanPoint1, dbChannel.ChannelPoint)
	require.Equal(t, testShortChanID1, dbChannel.ShortChannelID)
	require.Equal(t, peerID, dbChannel.PeerID)

	// Add a second channel for the same peer.
	channel2ID, err := store.AddChannel(
		ctx, testChanPoint2, testShortChanID2, peerID,
	)
	require.NoError(t, err)
	require.NotZero(t, channel2ID)

	// Add an online event for the channel.
	onlineEvent := &ChannelEvent{
		ChannelID: channelID,
		EventType: EventTypeOnline,
	}
	err = store.AddChannelEvent(ctx, onlineEvent)
	require.NoError(t, err)

	// Advance the clock for the next event.
	clock.SetTime(testTime.Add(time.Second))

	// Add an update event for the channel.
	localBalance := btcutil.Amount(1000)
	remoteBalance := btcutil.Amount(2000)
	updateEvent := &ChannelEvent{
		ChannelID:     channelID,
		EventType:     EventTypeUpdate,
		LocalBalance:  fn.Some(localBalance),
		RemoteBalance: fn.Some(remoteBalance),
	}
	err = store.AddChannelEvent(ctx, updateEvent)
	require.NoError(t, err)

	// Get the channel events and assert they are correct.
	events, err := store.GetChannelEvents(
		ctx, channelID, time.Unix(0, 0), time.Unix(3, 0),
	)
	require.NoError(t, err)
	require.Len(t, events, 2)

	requireEqualEvent(t, onlineEvent, testTime, events[0])
	requireEqualEvent(
		t, updateEvent, testTime.Add(time.Second), events[1],
	)

	// Advance the clock and add a sync event to verify the IsSync flag
	// round-trips correctly.
	clock.SetTime(testTime.Add(2 * time.Second))

	syncEvent := &ChannelEvent{
		ChannelID: channelID,
		EventType: EventTypeOnline,
		IsSync:    true,
	}
	err = store.AddChannelEvent(ctx, syncEvent)
	require.NoError(t, err)

	events, err = store.GetChannelEvents(
		ctx, channelID, time.Unix(0, 0), time.Unix(4, 0),
	)
	require.NoError(t, err)
	require.Len(t, events, 3)

	requireEqualEvent(
		t, syncEvent, testTime.Add(2*time.Second), events[2],
	)
}
