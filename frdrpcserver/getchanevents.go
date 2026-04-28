package frdrpcserver

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/faraday/chanevents"
	"github.com/lightninglabs/faraday/frdrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// maxChannelEventsLimit is the hard cap the server will return in a single
// GetChannelEvents response, regardless of what the client asks for. It also
// serves as the default when the request leaves max_events at zero.
const maxChannelEventsLimit = 10000

// GetChannelEvents implements the frdrpc.FaradayServerServer interface.
func (s *RPCServer) GetChannelEvents(ctx context.Context,
	req *frdrpc.ChannelEventsRequest) (*frdrpc.ChannelEventsResponse, error) {

	if req.ChanPoint == "" {
		return nil, status.Error(
			codes.InvalidArgument, "channel point required",
		)
	}

	if req.EndTime == 0 {
		return nil, status.Error(
			codes.InvalidArgument, "end_time is required",
		)
	}

	if req.StartTime > math.MaxInt64 || req.EndTime > math.MaxInt64 {
		return nil, status.Error(
			codes.InvalidArgument,
			"start_time and end_time must be <= MaxInt64",
		)
	}

	if req.StartTime > req.EndTime {
		return nil, status.Error(
			codes.InvalidArgument,
			"start_time must be <= end_time",
		)
	}

	channel, err := s.cfg.ChanEvents.GetChannel(ctx, req.ChanPoint)
	if err != nil {
		if errors.Is(err, chanevents.ErrUnknownChannel) {
			return nil, status.Errorf(
				codes.NotFound, "channel %s not found",
				req.ChanPoint,
			)
		}
		log.Errorf("GetChannel(%s): %v", req.ChanPoint, err)
		return nil, status.Error(
			codes.Internal, "failed to look up channel",
		)
	}

	startTime := time.Unix(int64(req.StartTime), 0)
	endTime := time.Unix(int64(req.EndTime), 0)

	limit := int32(maxChannelEventsLimit)
	if req.MaxEvents != 0 && req.MaxEvents < maxChannelEventsLimit {
		limit = int32(req.MaxEvents)
	}

	events, err := s.cfg.ChanEvents.GetChannelEvents(
		ctx, channel.ID, startTime, endTime, limit,
	)
	if err != nil {
		log.Errorf("GetChannelEvents(%s): %v", req.ChanPoint, err)
		return nil, status.Error(
			codes.Internal, "failed to query channel events",
		)
	}

	return &frdrpc.ChannelEventsResponse{
		Events: marshalRPCChannelEvents(events),
	}, nil
}

// marshalRPCChannelEvents converts a slice of chanevents.ChannelEvent into a
// slice of frdrpc.ChannelEvent.
func marshalRPCChannelEvents(
	events []*chanevents.ChannelEvent) []*frdrpc.ChannelEvent {

	rpcEvents := make([]*frdrpc.ChannelEvent, len(events))

	for i, event := range events {
		rpcEvent := &frdrpc.ChannelEvent{
			Timestamp: uint64(event.Timestamp.Unix()),
			EventType: rpcEventType(event.EventType),
		}

		event.LocalBalance.WhenSome(func(b btcutil.Amount) {
			rpcEvent.LocalBalance = uint64(b)
		})
		event.RemoteBalance.WhenSome(func(b btcutil.Amount) {
			rpcEvent.RemoteBalance = uint64(b)
		})

		rpcEvents[i] = rpcEvent
	}

	return rpcEvents
}

// rpcEventType maps a stored chanevents.EventType to its proto counterpart.
// A direct numeric cast would silently transmit any future stored type as an
// undefined proto enum variant; mapping unknown values to CHAN_EVENT_UNKNOWN
// keeps the wire format well-defined as the schema evolves.
func rpcEventType(e chanevents.EventType) frdrpc.ChannelEventType {
	switch e {
	case chanevents.EventTypeOnline:
		return frdrpc.ChannelEventType_CHAN_EVENT_ONLINE
	case chanevents.EventTypeOffline:
		return frdrpc.ChannelEventType_CHAN_EVENT_OFFLINE
	case chanevents.EventTypeUpdate:
		return frdrpc.ChannelEventType_CHAN_EVENT_UPDATE
	default:
		return frdrpc.ChannelEventType_CHAN_EVENT_UNKNOWN
	}
}
