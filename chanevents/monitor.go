package chanevents

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/fn/v2"
	"github.com/lightningnetwork/lnd/routing/route"
)

const (
	// retryInterval is the time to wait before retrying after a
	// transient error or while waiting for lnd to become ready.
	retryInterval = 5 * time.Second
)

var (
	// errMonitorAlreadyStarted is returned when the monitor is already
	// started.
	errMonitorAlreadyStarted = errors.New("monitor already started")

	// errMonitorNotStarted is returned when the monitor is not started.
	errMonitorNotStarted = errors.New("monitor not started")
)

// Monitor is an active component that listens to LND channel events and records
// them in the database.
type Monitor struct {
	started atomic.Bool

	// lnd is the lnd client that the monitor will use to subscribe to
	// channel events.
	lnd lndclient.LightningClient

	// store is the channel events store that the monitor will use to record
	// channel events.
	store *Store

	wg   sync.WaitGroup
	quit chan struct{}
}

// NewMonitor creates a new channel events monitor.
func NewMonitor(lnd lndclient.LightningClient, store *Store) *Monitor {
	return &Monitor{
		lnd:   lnd,
		store: store,
		quit:  make(chan struct{}),
	}
}

// Start starts the channel events monitor.
func (m *Monitor) Start(ctx context.Context) error {
	if !m.started.CompareAndSwap(false, true) {
		return errMonitorAlreadyStarted
	}

	log.Info("Starting channel events monitor")

	m.quit = make(chan struct{})

	m.wg.Add(1)
	go m.monitorLoop(ctx)

	return nil
}

// Stop stops the channel events monitor.
func (m *Monitor) Stop() error {
	if !m.started.CompareAndSwap(true, false) {
		return errMonitorNotStarted
	}

	log.Info("Stopping channel events monitor")

	close(m.quit)
	m.wg.Wait()

	return nil
}

// monitorLoop is the main loop of the channel events monitor. It waits for lnd
// to be fully synced, performs an initial state sync, and then subscribes to
// channel events. If the subscription fails or the stream breaks, it retries
// from the beginning.
func (m *Monitor) monitorLoop(ctx context.Context) {
	defer m.wg.Done()

	log.Info("Channel events monitor starting")

	var synced bool

	for {
		// Wait for lnd to be synced to chain, retrying on RPC errors.
		if !m.waitForReady(ctx) {
			return
		}

		// Initial state sync, only performed once.
		if !synced {
			if err := m.initialSync(ctx); err != nil {
				log.Errorf("Error during initial sync: %v", err)
			} else {
				synced = true
			}
		}

		// Subscribe and consume events until the stream breaks or an
		// error occurs.
		if !m.subscribe(ctx) {
			return
		}

		// Stream broke, wait before reconnecting.
		log.Infof("Reconnecting channel event subscription...")

		select {
		case <-time.After(retryInterval):
		case <-m.quit:
			return
		case <-ctx.Done():
			return
		}
	}
}

// waitForReady polls lnd's GetInfo until it reports SyncedToChain. It retries
// on transient RPC errors. It returns true when lnd is ready, or false if the
// monitor is shutting down.
func (m *Monitor) waitForReady(ctx context.Context) bool {
	for {
		info, err := m.lnd.GetInfo(ctx)
		if err != nil {
			log.Warnf("Error getting lnd info, retrying: %v", err)
		} else if info.SyncedToChain {
			return true
		} else {
			log.Infof("Waiting for lnd to sync to chain...")
		}

		select {
		case <-time.After(retryInterval):
		case <-m.quit:
			return false
		case <-ctx.Done():
			return false
		}
	}
}

// subscribe subscribes to lnd channel events and processes them until the
// stream breaks or an error occurs. It returns true on transient failures
// (caller should retry) or false if the monitor is shutting down.
func (m *Monitor) subscribe(ctx context.Context) bool {
	eventChan, errChan, err := m.lnd.SubscribeChannelEvents(ctx)
	if err != nil {
		log.Errorf("Error subscribing to channel events: %v", err)

		// Return true to signal the caller to retry.
		return true
	}

	for {
		select {
		case event, ok := <-eventChan:
			if !ok {
				log.Warn("Channel event stream closed")
				return true
			}
			if err := m.handleChannelEvent(ctx, event); err != nil {
				log.Errorf("Error handling channel event: %v",
					err)
			}

		case err, ok := <-errChan:
			if !ok {
				log.Warn("Channel event error stream " +
					"closed")

				return true
			}
			log.Errorf("Error from channel event "+
				"subscription: %v", err)

			return true

		case <-m.quit:
			log.Info("Channel events monitor stopping")
			return false

		case <-ctx.Done():
			log.Info("Channel events monitor stopping")
			return false
		}
	}
}

// initialSync performs an initial sync of the channel state. It queries lnd for
// all known channels (open and closed) and records their current state in the
// database. This ensures that any channel events that occurred while faraday
// was offline are accounted for: even though individual events are lost, the
// latest state is captured as a baseline. Events recorded during initial sync
// are marked with IsSync=true to distinguish them from real-time events
// received via the subscription.
func (m *Monitor) initialSync(ctx context.Context) error {
	log.Info("Performing initial sync of channel state")

	closedChannels, err := m.lnd.ClosedChannels(ctx)
	if err != nil {
		return fmt.Errorf("error listing closed channels: %w", err)
	}

	for _, channel := range closedChannels {
		// Abort if the context has been cancelled.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Channels that didn't confirm onchain will be present here,
		// but don't have a channel ID. We skip those.
		if channel.ChannelID == 0 {
			log.Debugf("Skipping closed channel with no "+
				"channel ID: %s", channel.ChannelPoint)

			continue
		}

		err := m.addChannel(
			ctx, channel.PubKeyBytes, channel.ChannelPoint,
			channel.ChannelID,
		)

		if err != nil {
			log.Errorf("error adding closed channel %s: %v",
				channel.ChannelPoint, err)

			continue
		}

		dbChan, err := m.store.GetChannel(ctx, channel.ChannelPoint)
		if err != nil {
			log.Errorf("error getting closed channel %s from db: %v",
				channel.ChannelPoint, err)

			continue
		}

		if err := m.store.AddChannelEvent(ctx, &ChannelEvent{
			ChannelID: dbChan.ID,
			EventType: EventTypeOffline,
			IsSync:    true,
		}); err != nil {
			log.Errorf("error adding offline event for closed "+
				"channel %s: %v", channel.ChannelPoint, err)
		}
	}

	channels, err := m.lnd.ListChannels(ctx, false, false)
	if err != nil {
		return fmt.Errorf("error listing channels: %w", err)
	}

	for _, channel := range channels {
		// Abort if the context has been cancelled.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// We make sure the channel exists in the store.
		err := m.addChannel(
			ctx, channel.PubKeyBytes, channel.ChannelPoint,
			channel.ChannelID,
		)
		if err != nil {
			log.Errorf("error adding channel %s: %v",
				channel.ChannelPoint, err)

			continue
		}

		dbChan, err := m.store.GetChannel(ctx, channel.ChannelPoint)
		if err != nil {
			log.Errorf("error getting channel %s from db: %v",
				channel.ChannelPoint, err)

			continue
		}

		eventType := EventTypeOffline
		if channel.Active {
			eventType = EventTypeOnline
		}

		if err := m.store.AddChannelEvent(ctx, &ChannelEvent{
			ChannelID: dbChan.ID,
			EventType: eventType,
			IsSync:    true,
		}); err != nil {
			log.Errorf("error adding event for channel %s: %v",
				channel.ChannelPoint, err)
		}

		// We add the update event separately from the online/offline
		// event above, because each event type serves a different
		// purpose: the online/offline event tracks channel
		// availability, while the update event captures a balance
		// snapshot. Keeping them as distinct records allows querying
		// availability and balance history independently.
		if err := m.store.AddChannelEvent(ctx, &ChannelEvent{
			ChannelID:     dbChan.ID,
			EventType:     EventTypeUpdate,
			LocalBalance:  fn.Some(channel.LocalBalance),
			RemoteBalance: fn.Some(channel.RemoteBalance),
			IsSync:        true,
		}); err != nil {
			log.Errorf("error adding event for channel %s: %v",
				channel.ChannelPoint, err)
		}
	}

	return nil
}

// addChannel adds a channel and its peer to the store.
func (m *Monitor) addChannel(ctx context.Context, pubKeyBytes route.Vertex,
	channelPoint string, channelID uint64) error {

	// Check if the channel already exists.
	channel, err := m.store.GetChannel(ctx, channelPoint)
	if err != nil && !errors.Is(err, ErrUnknownChannel) {
		return fmt.Errorf("error getting channel %s: %w",
			channelPoint, err)
	}
	if channel != nil {
		// Channel already exists, nothing to do.
		return nil
	}

	// Check if peer already exists.
	peer, err := m.store.GetPeer(ctx, pubKeyBytes.String())
	if err != nil && !errors.Is(err, errUnknownPeer) {
		return fmt.Errorf("error getting peer %s: %w",
			pubKeyBytes, err)
	}

	var peerID int64
	if peer != nil {
		peerID = peer.ID
	} else {
		peerID, err = m.store.AddPeer(
			ctx, pubKeyBytes.String(),
		)
		if err != nil {
			return fmt.Errorf("error adding peer %s: %w",
				pubKeyBytes, err)
		}
	}

	_, err = m.store.AddChannel(ctx, channelPoint, channelID, peerID)
	if err != nil {
		return fmt.Errorf("error adding channel %s: %w",
			channelPoint, err)
	}

	log.Infof("Added channel %s to db", channelPoint)

	return nil
}

// handleChannelEvent handles a single channel event.
func (m *Monitor) handleChannelEvent(ctx context.Context,
	event *lndclient.ChannelEventUpdate) error {

	switch event.UpdateType {
	case lndclient.OpenChannelUpdate:
		openChannel := event.OpenedChannelInfo
		if openChannel == nil {
			return fmt.Errorf("open_channel event is nil")
		}

		log.Debugf("Handling open channel event: %+v", openChannel)

		// We add the new channel to the store.
		if err := m.addChannel(
			ctx, openChannel.PubKeyBytes, openChannel.ChannelPoint,
			openChannel.ChannelID,
		); err != nil {
			return err
		}

		// Now add the online and update events.
		dbChan, err := m.store.GetChannel(ctx, openChannel.ChannelPoint)
		if err != nil {
			return err
		}

		if err := m.store.AddChannelEvent(ctx, &ChannelEvent{
			ChannelID: dbChan.ID,
			EventType: EventTypeOnline,
		}); err != nil {
			return err
		}

		return m.addUpdateEvent(ctx, openChannel)

	case lndclient.ClosedChannelUpdate:
		if event.ClosedChannelInfo == nil {
			return fmt.Errorf("closed_channel event is nil")
		}

		log.Debugf("Handling offline channel event: %+v",
			event.ClosedChannelInfo)

		return m.addOfflineEvent(ctx,
			event.ClosedChannelInfo.ChannelPoint)

	case lndclient.ActiveChannelUpdate:
		log.Debugf("Handling active channel event: %v",
			event.ChannelPoint)

		return m.addOnlineEvent(ctx, event.ChannelPoint.String())

	case lndclient.InactiveChannelUpdate:
		log.Debugf("Handling offline channel event: %v",
			event.ChannelPoint)

		return m.addOfflineEvent(ctx, event.ChannelPoint.String())

	case lndclient.PendingOpenChannelUpdate:
		log.Debugf("Ignoring pending channel event: %v",
			event.ChannelPoint)

		return nil

	case lndclient.StateChannelUpdate:
		if event.UpdatedChannelInfo == nil {
			return fmt.Errorf("state_update event is nil")
		}

		log.Debugf("Handling channel update event: %+v",
			event.UpdatedChannelInfo)

		return m.addUpdateEvent(ctx, event.UpdatedChannelInfo)
	}

	return nil
}

// addOnlineEvent adds an online event for a channel.
func (m *Monitor) addOnlineEvent(ctx context.Context,
	channelPoint string) error {

	channel, err := m.store.GetChannel(ctx, channelPoint)
	if err != nil {
		return fmt.Errorf("error getting channel %s: %w", channelPoint,
			err)
	}

	log.Infof("Adding online event for channel %s", channelPoint)

	return m.store.AddChannelEvent(ctx, &ChannelEvent{
		ChannelID: channel.ID,
		EventType: EventTypeOnline,
	})
}

// addOfflineEvent adds an offline event for a channel.
func (m *Monitor) addOfflineEvent(ctx context.Context,
	channelPoint string) error {

	channel, err := m.store.GetChannel(ctx, channelPoint)
	if err != nil {
		return fmt.Errorf("error getting channel %s: %w", channelPoint,
			err)
	}

	log.Infof("Adding offline event for channel %s", channelPoint)

	return m.store.AddChannelEvent(ctx, &ChannelEvent{
		ChannelID: channel.ID,
		EventType: EventTypeOffline,
	})
}

// addUpdateEvent adds an update event for a channel.
func (m *Monitor) addUpdateEvent(ctx context.Context,
	channelInfo *lndclient.ChannelInfo) error {

	channel, err := m.store.GetChannel(ctx, channelInfo.ChannelPoint)
	if err != nil {
		return fmt.Errorf("error getting channel %s: %w",
			channelInfo.ChannelPoint, err)
	}

	log.Tracef("Adding update event for channel %s",
		channelInfo.ChannelPoint)

	return m.store.AddChannelEvent(ctx, &ChannelEvent{
		ChannelID: channel.ID,
		EventType: EventTypeUpdate,
		LocalBalance: fn.Some(
			btcutil.Amount(channelInfo.LocalBalance),
		),
		RemoteBalance: fn.Some(
			btcutil.Amount(channelInfo.RemoteBalance),
		),
	})
}
