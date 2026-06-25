// Package chanevents contains functions for monitoring and storing channel
// events such as online/offline and balance updates.
package chanevents

import (
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightningnetwork/lnd/fn/v2"
)

// Config holds the configuration options for channel event pruning. See the
// README for storage sizing guidance.
type Config struct {
	// MaxEvents is the maximum number of channel events to retain. Once the
	// table exceeds this count, the oldest events are pruned. This operates
	// as a hard ceiling on database size to prevent disk filling. A value
	// of 0 disables this limit.
	MaxEvents uint64 `long:"max-events" description:"The maximum number of channel events to retain before pruning the oldest events. This limit acts as a hard ceiling to prevent disk filling. A value of 0 disables pruning based on the number of events."`

	// Retention is the minimum duration of channel events to keep. Events
	// older than this window are pruned, even if the max-events limit is
	// not exceeded. If max-events is exceeded, newer events can still be
	// pruned to enforce the size ceiling. A value of 0 disables this limit.
	Retention time.Duration `long:"retention" description:"The minimum duration of channel events to keep. Events older than this window are pruned, even if the max-events limit is not exceeded. A value of 0 disables pruning based on age."`
}

// EventType is an enum for the different types of channel events.
type EventType int16

const (
	// EventTypeUnknown is the unknown event type.
	EventTypeUnknown EventType = 0

	// EventTypeOnline is the online event type.
	EventTypeOnline EventType = 1

	// EventTypeOffline is the offline event type.
	EventTypeOffline EventType = 2

	// EventTypeUpdate is the balance update event type.
	EventTypeUpdate EventType = 3
)

// String returns the string representation of the event type.
func (e EventType) String() string {
	switch e {
	case EventTypeOnline:
		return "online"

	case EventTypeOffline:
		return "offline"

	case EventTypeUpdate:
		return "update"

	default:
		return "unknown"
	}
}

// EventTypeFromString returns the event type from a string.
func EventTypeFromString(s string) EventType {
	switch s {
	case "online":
		return EventTypeOnline

	case "offline":
		return EventTypeOffline

	case "update":
		return EventTypeUpdate

	default:
		return EventTypeUnknown
	}
}

// Peer is the application-level representation of a peer.
type Peer struct {
	// ID is the database ID of the peer.
	ID int64

	// PubKey is the public key of the peer.
	PubKey string
}

// Channel is the application-level representation of a channel.
type Channel struct {
	// ID is the database ID of the channel.
	ID int64

	// ChannelPoint is the channel point of the channel.
	ChannelPoint string

	// ShortChannelID is the short channel ID of the channel.
	ShortChannelID uint64

	// PeerID is the database ID of the peer that this channel is with.
	PeerID int64
}

// ChannelEvent is the application-level representation of a channel event.
type ChannelEvent struct {
	// ID is the database ID of the event.
	ID int64

	// ChannelID is the database ID of the channel that this event is
	// associated with.
	ChannelID int64

	// EventType is the type of the event.
	EventType EventType

	// Timestamp is the time that the event occurred.
	Timestamp time.Time

	// LocalBalance is the local balance of the channel at the time of the
	// event. This is only populated for balance update events.
	LocalBalance fn.Option[btcutil.Amount]

	// RemoteBalance is the remote balance of the channel at the time of the
	// event. This is only populated for balance update events.
	RemoteBalance fn.Option[btcutil.Amount]

	// IsSync indicates whether this event was recorded during an initial
	// sync rather than from a live subscription.
	IsSync bool
}
