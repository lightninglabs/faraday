package chanevents

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/faraday/db"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/clock"
	"github.com/lightningnetwork/lnd/fn/v2"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/lightningnetwork/lnd/sqldb/v2"
	"github.com/stretchr/testify/require"
)

// benchFixtureVersion bumps whenever the populator changes shape so stale
// fixtures from older runs don't poison new benchmark numbers.
const benchFixtureVersion = 1

func BenchmarkEffectiveUptimeMultiChan(b *testing.B) {
	const (
		numPeers          = 30
		channelsPerPeer   = 5
		updatesPerChannel = 130
		seed              = 4242
	)

	ctx := context.Background()
	clk := clock.NewTestClock(testTime)

	startTime := testTime.Add(time.Hour)
	endTime := startTime.Add(24 * time.Hour)

	store, openChannels := loadOrPopulateMultiChanStore(
		b, ctx, clk, numPeers, channelsPerPeer, updatesPerChannel, seed,
		startTime, endTime,
	)

	stub := &stubLndChannelClient{openChannels: openChannels}
	analyzer := NewForwardingAnalyzer(
		store, lndclient.LndServices{
			Client: stub,
		},
	)

	for b.Loop() {
		_, err := analyzer.EffectiveUptime(
			ctx, startTime, endTime, 0, 0,
		)
		require.NoError(b, err)
	}
}

func loadOrPopulateMultiChanStore(b *testing.B, ctx context.Context,
	clk clock.Clock, numPeers, channelsPerPeer, updatesPerChannel, seed int,
	startTime, endTime time.Time) (*Store, []lndclient.ChannelInfo) {

	fixtureName := fmt.Sprintf("faraday-bench-eu-mc-v%d-p%d-c%d-u%d-s%d.s"+
		"qlite", benchFixtureVersion, numPeers, channelsPerPeer,
		updatesPerChannel, seed)
	fixturePath := filepath.Join(os.TempDir(), fixtureName)
	workingPath := filepath.Join(b.TempDir(), "tmp.db")

	openChannels := multiChanChannelList(b, numPeers, channelsPerPeer)

	if _, err := os.Stat(fixturePath); err == nil {
		require.NoError(b, copyFile(fixturePath, workingPath))

		return openBenchStore(b, workingPath, clk, true), openChannels
	}

	store := openBenchStore(b, workingPath, clk, false)
	populateMultiChanData(
		b, ctx, store, numPeers, channelsPerPeer, updatesPerChannel,
		seed, startTime, endTime,
	)
	_, err := store.ExecContext(
		ctx, fmt.Sprintf("VACUUM INTO '%s'", fixturePath),
	)
	require.NoError(b, err)

	return store, openChannels
}

func multiChanChannelList(b *testing.B, numPeers,
	channelsPerPeer int) []lndclient.ChannelInfo {

	openChannels := make(
		[]lndclient.ChannelInfo, 0, numPeers*channelsPerPeer,
	)
	for i := range numPeers {
		pubKey := benchPubKey(i)
		vertex, err := route.NewVertexFromStr(pubKey)
		require.NoError(b, err)

		for c := range channelsPerPeer {
			openChannels = append(
				openChannels, lndclient.ChannelInfo{
					ChannelID: uint64(
						100_000 + i*channelsPerPeer + c,
					),
					PubKeyBytes: vertex,
				},
			)
		}
	}

	return openChannels
}

func populateMultiChanData(b *testing.B, ctx context.Context, store *Store,
	numPeers, channelsPerPeer, updatesPerChannel, seed int, startTime,
	endTime time.Time) {

	windowNanos := endTime.Sub(startTime).Nanoseconds()
	rng := rand.New(rand.NewSource(int64(seed)))

	for i := range numPeers {
		peerID, err := store.AddPeer(ctx, benchPubKey(i))
		require.NoError(b, err)

		for c := range channelsPerPeer {
			chanPoint := fmt.Sprintf("bench_mc_txid:%d:%d", i, c)
			scid := uint64(100_000 + i*channelsPerPeer + c)
			chanID, err := store.AddChannel(
				ctx, chanPoint, scid, peerID,
			)
			require.NoError(b, err)

			err = store.AddChannelEvent(
				ctx, &ChannelEvent{
					ChannelID: chanID,
					EventType: EventTypeUpdate,
					Timestamp: testTime,
					LocalBalance: fn.Some(
						btcutil.Amount(1_000_000),
					),
					RemoteBalance: fn.Some(
						btcutil.Amount(1_000_000),
					),
				},
			)
			require.NoError(b, err)

			timestamps := make([]time.Time, updatesPerChannel)
			for j := range timestamps {
				offset := rng.Int63n(windowNanos)
				timestamps[j] = startTime.Add(
					time.Duration(offset),
				)
			}
			sort.Slice(
				timestamps,
				func(a, c int) bool {
					return timestamps[a].Before(
						timestamps[c],
					)
				},
			)

			for _, ts := range timestamps {
				evType := []EventType{
					EventTypeOnline, EventTypeOffline,
					EventTypeUpdate,
				}[rng.Intn(3)]

				ev := &ChannelEvent{
					ChannelID: chanID,
					EventType: evType,
					Timestamp: ts,
				}
				if evType == EventTypeUpdate {
					ev.LocalBalance = fn.Some(
						btcutil.Amount(
							rng.Int63n(2_000_000),
						),
					)
					ev.RemoteBalance = fn.Some(
						btcutil.Amount(
							rng.Int63n(2_000_000),
						),
					)
				}
				require.NoError(
					b, store.AddChannelEvent(ctx, ev),
				)
			}
		}
	}
}

// BenchmarkEffectiveUptime exercises the K² pair walk against a realistic
// fleet: 50 peers, one channel per peer, 1000 random events per channel within
// the analysis window. The benchmark targets the analyzer's per-pair restream
// cost so the cache layer's amortisation shows up as a wall-clock delta when
// comparing the analyzer-only commit against the cache commit.
func BenchmarkEffectiveUptime(b *testing.B) {
	const (
		numPeers          = 100
		updatesPerChannel = 200
		seed              = 42
	)

	ctx := context.Background()
	clk := clock.NewTestClock(testTime)

	startTime := testTime.Add(time.Hour)
	endTime := startTime.Add(24 * time.Hour)

	store, openChannels := loadOrPopulateBenchStore(
		b, ctx, clk, numPeers, updatesPerChannel, seed, startTime,
		endTime,
	)

	stub := &stubLndChannelClient{openChannels: openChannels}
	analyzer := NewForwardingAnalyzer(
		store, lndclient.LndServices{
			Client: stub,
		},
	)

	for b.Loop() {
		_, err := analyzer.EffectiveUptime(
			ctx, startTime, endTime, 0, 0,
		)
		require.NoError(b, err)
	}
}

// loadOrPopulateBenchStore returns a Store ready for the analyzer benchmark,
// caching the fixture in $TMPDIR to avoid re-running the SQLite population on
// every benchmark invocation.
func loadOrPopulateBenchStore(b *testing.B, ctx context.Context,
	clk clock.Clock, numPeers, updatesPerChannel, seed int, startTime,
	endTime time.Time) (*Store, []lndclient.ChannelInfo) {

	fixtureName := fmt.Sprintf("faraday-bench-eu-v%d-p%d-u%d-s%d.sqlite",
		benchFixtureVersion, numPeers, updatesPerChannel, seed)
	fixturePath := filepath.Join(os.TempDir(), fixtureName)
	workingPath := filepath.Join(b.TempDir(), "tmp.db")

	openChannels := benchChannelList(b, numPeers)

	if _, err := os.Stat(fixturePath); err == nil {
		b.Logf("Reusing bench fixture %s", fixturePath)
		require.NoError(b, copyFile(fixturePath, workingPath))

		return openBenchStore(b, workingPath, clk, true), openChannels
	}

	b.Logf("Populating bench fixture (will cache at %s)", fixturePath)
	store := openBenchStore(b, workingPath, clk, false)
	populateBenchData(
		b, ctx, store, numPeers, updatesPerChannel, seed, startTime,
		endTime,
	)

	// VACUUM INTO writes a clean, WAL-free snapshot atomically so the
	// fixture is self-contained.
	_, err := store.ExecContext(
		ctx, fmt.Sprintf("VACUUM INTO '%s'", fixturePath),
	)
	require.NoError(b, err)

	return store, openChannels
}

// openBenchStore opens a SqliteStore at the given path and wraps it in a
// chanevents.Store. When reuse is true the migrations are skipped because the
// fixture was migrated when it was originally created.
func openBenchStore(b *testing.B, dbPath string, clk clock.Clock,
	reuse bool) *Store {

	sqlDB, err := sqldb.NewSqliteStore(
		&sqldb.SqliteConfig{
			SkipMigrations: reuse,
		},
		dbPath,
	)
	require.NoError(b, err)

	if !reuse {
		require.NoError(
			b, sqldb.ApplyAllMigrations(
				sqlDB, db.FaradayMigrationSets,
			),
		)
	}

	b.Cleanup(func() {
		require.NoError(b, sqlDB.DB.Close())
	})

	return createStore(b, sqlDB.BaseDB, clk)
}

// benchChannelList reconstructs the lnd-stub ChannelInfo set from the bench
// parameters. The pubkey and scid layouts mirror populateBenchData so the
// fixture-reuse path doesn't need to read them back from the store.
func benchChannelList(b *testing.B, numPeers int) []lndclient.ChannelInfo {
	openChannels := make([]lndclient.ChannelInfo, numPeers)
	for i := range numPeers {
		pubKey := benchPubKey(i)
		vertex, err := route.NewVertexFromStr(pubKey)
		require.NoError(b, err)

		openChannels[i] = lndclient.ChannelInfo{
			ChannelID:   uint64(1_000 + i),
			PubKeyBytes: vertex,
		}
	}

	return openChannels
}

// populateBenchData inserts the bench dataset: one peer per index, one channel
// per peer, a seed Update before startTime so the initial-state walk has a
// baseline, and updatesPerChannel random events per channel inside the window.
func populateBenchData(b *testing.B, ctx context.Context, store *Store,
	numPeers, updatesPerChannel, seed int, startTime, endTime time.Time) {

	windowNanos := endTime.Sub(startTime).Nanoseconds()
	rng := rand.New(rand.NewSource(int64(seed)))

	for i := range numPeers {
		peerID, err := store.AddPeer(ctx, benchPubKey(i))
		require.NoError(b, err)

		chanPoint := fmt.Sprintf("bench_txid:%d", i)
		scid := uint64(1_000 + i)
		chanID, err := store.AddChannel(ctx, chanPoint, scid, peerID)
		require.NoError(b, err)

		err = store.AddChannelEvent(
			ctx, &ChannelEvent{
				ChannelID: chanID,
				EventType: EventTypeUpdate,
				Timestamp: testTime,
				LocalBalance: fn.Some(
					btcutil.Amount(1_000_000),
				),
				RemoteBalance: fn.Some(
					btcutil.Amount(1_000_000),
				),
			},
		)
		require.NoError(b, err)

		timestamps := make([]time.Time, updatesPerChannel)
		for j := range timestamps {
			offset := rng.Int63n(windowNanos)
			timestamps[j] = startTime.Add(time.Duration(offset))
		}
		sort.Slice(
			timestamps,
			func(a, c int) bool {
				return timestamps[a].Before(timestamps[c])
			},
		)

		for _, ts := range timestamps {
			evType := []EventType{
				EventTypeOnline, EventTypeOffline,
				EventTypeUpdate,
			}[rng.Intn(3)]

			ev := &ChannelEvent{
				ChannelID: chanID,
				EventType: evType,
				Timestamp: ts,
			}
			if evType == EventTypeUpdate {
				ev.LocalBalance = fn.Some(
					btcutil.Amount(
						rng.Int63n(2_000_000),
					),
				)
				ev.RemoteBalance = fn.Some(
					btcutil.Amount(
						rng.Int63n(2_000_000),
					),
				)
			}
			require.NoError(b, store.AddChannelEvent(ctx, ev))
		}
	}
}

// benchPubKey synthesises a 66-char hex pubkey for peer index i.
// NewVertexFromStr validates length and hex only, not curve membership.
func benchPubKey(i int) string {
	return fmt.Sprintf("02%064x", i+1)
}

// copyFile copies src to dst, truncating dst if it exists.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return out.Sync()
}
