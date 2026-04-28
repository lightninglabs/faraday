package chanevents

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/faraday/db/sqlc"
	"github.com/lightningnetwork/lnd/clock"
	"github.com/lightningnetwork/lnd/fn/v2"
	"github.com/lightningnetwork/lnd/sqldb/v2"
)

var (
	errUnknownPeer = errors.New("unknown peer")

	// ErrUnknownChannel is returned by GetChannel when the requested
	// channel point is not present in the store.
	ErrUnknownChannel = errors.New("unknown channel")
)

// Queries is a subset of the sqlc.Queries interface that can be used to
// interact with the peers, channels and channel_events tables.
type Queries interface {
	InsertPeer(ctx context.Context, pubkey string) (int64, error)

	GetPeerByPubKey(ctx context.Context, pubkey string) (sqlc.Peer, error)

	InsertChannel(ctx context.Context,
		arg sqlc.InsertChannelParams) (int64, error)

	GetChannelByChanPoint(ctx context.Context,
		channelPoint string) (sqlc.Channel, error)

	GetChannelByShortChanID(ctx context.Context,
		shortChannelID int64) (sqlc.Channel, error)

	InsertChannelEvent(ctx context.Context,
		arg sqlc.InsertChannelEventParams) error

	GetChannelEvents(ctx context.Context,
		arg sqlc.GetChannelEventsParams) ([]sqlc.ChannelEvent, error)
}

// Store provides access to the db for channel events.
type Store struct {
	// db is all the higher level queries that the SQLStore has access to in
	// order to implement all its CRUD logic.
	db BatchedSQLQueries

	// BaseDB represents the underlying database connection.
	*sqldb.BaseDB

	clock clock.Clock
}

// BatchedSQLQueries combines the SQLQueries interface with the BatchedTx
// interface, allowing for multiple queries to be executed in single SQL
// transaction.
type BatchedSQLQueries interface {
	SQLQueries

	sqldb.BatchedTx[SQLQueries]
}

// SQLQueries is a subset of the sqlc.Queries interface that can be used to
// interact with various chanevents tables.
type SQLQueries interface {
	sqldb.BaseQuerier

	Queries
}

type SQLQueriesExecutor[T sqldb.BaseQuerier] struct {
	*sqldb.TransactionExecutor[T]

	SQLQueries
}

// NewStore creates a new SQLStore instance given an open SQLQueries storage
// backend.
func NewStore(sqlDB *sqldb.BaseDB, queries *sqlc.Queries,
	clock clock.Clock) *Store {

	txExecutor := sqldb.NewTransactionExecutor(
		sqlDB,
		func(tx *sql.Tx) SQLQueries {
			return queries.WithTx(tx)
		},
	)

	executor := &SQLQueriesExecutor[SQLQueries]{
		TransactionExecutor: txExecutor,
		SQLQueries:          queries,
	}

	return &Store{
		db:     executor,
		BaseDB: sqlDB,
		clock:  clock,
	}
}

// AddPeer adds a new peer to the database.
func (s *Store) AddPeer(ctx context.Context, pubkey string) (int64, error) {
	id, err := s.db.InsertPeer(ctx, pubkey)
	if err != nil {
		return 0, fmt.Errorf("failed to insert peer: %w", err)
	}

	return id, nil
}

// GetPeer retrieves a peer by their public key.
func (s *Store) GetPeer(ctx context.Context, pubkey string) (*Peer, error) {
	dbPeer, err := s.db.GetPeerByPubKey(ctx, pubkey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errUnknownPeer
		}

		return nil, fmt.Errorf("failed to get peer: %w", err)
	}

	return &Peer{
		ID:     dbPeer.ID,
		PubKey: dbPeer.Pubkey,
	}, nil
}

// int64ToSCID converts an int64 to a uint64 ShortChannelID. The BOLT spec
// encodes SCIDs as uint64, but SQL only supports signed int64. We preserve the
// bits, which means SCIDs with the high bit set will appear negative in the
// database. Direct SQL queries (e.g. ORDER BY short_channel_id) will not sort
// these correctly, but round-tripping through Go preserves the value.
func int64ToSCID(i int64) uint64 {
	return uint64(i)
}

// scidToInt64 converts a uint64 ShortChannelID to an int64 for SQL storage.
func scidToInt64(u uint64) int64 {
	return int64(u)
}

// AddChannel adds a new channel for a peer.
func (s *Store) AddChannel(ctx context.Context, channelPoint string,
	shortChannelID uint64, peerID int64) (int64, error) {

	id, err := s.db.InsertChannel(
		ctx, sqlc.InsertChannelParams{
			ChannelPoint:   channelPoint,
			ShortChannelID: scidToInt64(shortChannelID),
			PeerID:         peerID,
		},
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert channel: %w", err)
	}

	return id, nil
}

// GetChannel retrieves a channel by its channel point.
func (s *Store) GetChannel(ctx context.Context, channelPoint string) (*Channel,
	error) {

	dbChannel, err := s.db.GetChannelByChanPoint(ctx, channelPoint)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUnknownChannel
		}

		return nil, fmt.Errorf("failed to get channel: %w", err)
	}

	return &Channel{
		ID:             dbChannel.ID,
		ChannelPoint:   dbChannel.ChannelPoint,
		ShortChannelID: int64ToSCID(dbChannel.ShortChannelID),
		PeerID:         dbChannel.PeerID,
	}, nil
}

// AddChannelEvent adds a new channel event.
func (s *Store) AddChannelEvent(ctx context.Context,
	event *ChannelEvent) error {

	var localBalance sql.NullInt64
	event.LocalBalance.WhenSome(
		func(b btcutil.Amount) {
			localBalance.Int64 = int64(b)
			localBalance.Valid = true
		},
	)

	var remoteBalance sql.NullInt64
	event.RemoteBalance.WhenSome(
		func(b btcutil.Amount) {
			remoteBalance.Int64 = int64(b)
			remoteBalance.Valid = true
		},
	)

	timestamp := event.Timestamp.UTC()
	if timestamp.IsZero() {
		timestamp = s.clock.Now().UTC()
	}

	err := s.db.InsertChannelEvent(
		ctx, sqlc.InsertChannelEventParams{
			ChannelID:        event.ChannelID,
			EventType:        int16(event.EventType),
			Timestamp:        timestamp,
			LocalBalanceSat:  localBalance,
			RemoteBalanceSat: remoteBalance,
			IsSync:           event.IsSync,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to insert channel event: %w", err)
	}

	return nil
}

// GetChannelEvents retrieves up to limit events for a channel within the
// half-open time range [startTime, endTime). Callers paginate by re-querying
// with startTime set to the timestamp of the last event returned, and must
// supply a positive limit; the RPC layer is responsible for bounding it.
func (s *Store) GetChannelEvents(ctx context.Context, channelID int64,
	startTime, endTime time.Time, limit int32) ([]*ChannelEvent, error) {

	dbEvents, err := s.db.GetChannelEvents(
		ctx, sqlc.GetChannelEventsParams{
			ChannelID:   channelID,
			Timestamp:   startTime.UTC(),
			Timestamp_2: endTime.UTC(),
			Limit:       limit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get channel events: %w", err)
	}

	events := make([]*ChannelEvent, len(dbEvents))
	for i, dbEvent := range dbEvents {
		events[i] = marshalChannelEvent(dbEvent)
	}

	return events, nil
}

// marshalChannelEvent converts a db channel event into our internal type.
func marshalChannelEvent(dbEvent sqlc.ChannelEvent) *ChannelEvent {
	var localBalance fn.Option[btcutil.Amount]
	if dbEvent.LocalBalanceSat.Valid {
		amt := btcutil.Amount(dbEvent.LocalBalanceSat.Int64)
		localBalance = fn.Some(amt)
	}

	var remoteBalance fn.Option[btcutil.Amount]
	if dbEvent.RemoteBalanceSat.Valid {
		amt := btcutil.Amount(dbEvent.RemoteBalanceSat.Int64)
		remoteBalance = fn.Some(amt)
	}

	return &ChannelEvent{
		ID:            dbEvent.ID,
		ChannelID:     dbEvent.ChannelID,
		EventType:     EventType(dbEvent.EventType),
		Timestamp:     dbEvent.Timestamp.UTC(),
		LocalBalance:  localBalance,
		RemoteBalance: remoteBalance,
		IsSync:        dbEvent.IsSync,
	}
}
