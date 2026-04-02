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

	require.Equal(t, onlineEvent.EventType, events[0].EventType)
	require.Equal(t, testTime.Unix(), events[0].Timestamp.Unix())
	require.True(t, events[0].LocalBalance.IsNone())
	require.True(t, events[0].RemoteBalance.IsNone())

	require.Equal(t, updateEvent.EventType, events[1].EventType)
	require.Equal(
		t, testTime.Add(time.Second).Unix(), events[1].Timestamp.Unix(),
	)
	require.Equal(t, updateEvent.LocalBalance, events[1].LocalBalance)
	require.Equal(t, updateEvent.RemoteBalance, events[1].RemoteBalance)
}
