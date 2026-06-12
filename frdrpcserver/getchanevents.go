package frdrpcserver

import (
	"context"
	"errors"
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

// GetChannelEvents serves a paginated read of a channel's events. A zero
// end_time defaults to the server's current time, max_events is clamped to
// the server's hard cap, and an unknown channel point yields NotFound.
func (s *RPCServer) GetChannelEvents(ctx context.Context,
	req *frdrpc.ChannelEventsRequest) (*frdrpc.ChannelEventsResponse,
	error) {

	log.Debugf("[GetChannelEvents]: chan_point=%s, start_time=%d, "+
		"end_time=%d, max_events=%d, last_id=%d", req.ChanPoint,
		req.StartTime, req.EndTime, req.MaxEvents, req.LastId)

	if req.ChanPoint == "" {
		return nil, status.Error(
			codes.InvalidArgument, "channel point required",
		)
	}

	if req.StartTime < 0 || req.EndTime < 0 {
		return nil, status.Error(
			codes.InvalidArgument,
			"start_time and end_time must be >= 0",
		)
	}

	startTime := time.Unix(req.StartTime, 0)
	endTime := time.Now()
	if req.EndTime != 0 {
		endTime = time.Unix(req.EndTime, 0)
	}

	if startTime.After(endTime) {
		return nil, status.Error(
			codes.InvalidArgument, "start_time must be <= end_time",
		)
	}

	if req.LastId < 0 {
		return nil, status.Error(
			codes.InvalidArgument, "last_id must be >= 0",
		)
	}

	channel, err := s.cfg.ChanEvents.GetChannel(ctx, req.ChanPoint)
	if err != nil {
		if errors.Is(err, chanevents.ErrUnknownChannel) {
			return nil, status.Errorf(codes.NotFound, "channel %s "+
				"not found", req.ChanPoint)
		}
		log.Errorf("GetChannel(%s): %v", req.ChanPoint, err)

		return nil, status.Error(
			codes.Internal, "failed to look up channel",
		)
	}

	limit := int32(maxChannelEventsLimit)
	if req.MaxEvents != 0 && req.MaxEvents < maxChannelEventsLimit {
		limit = int32(req.MaxEvents)
	}

	events, err := s.cfg.ChanEvents.GetChannelEvents(
		ctx, channel.ID, req.LastId, startTime, endTime, limit,
	)
	if err != nil {
		log.Errorf("GetChannelEvents(%s): %v", req.ChanPoint, err)

		return nil, status.Error(
			codes.Internal, "failed to query channel events",
		)
	}

	resp := &frdrpc.ChannelEventsResponse{
		Events:  marshalRPCChannelEvents(events),
		HasMore: int32(len(events)) == limit,
	}
	if n := len(events); n > 0 {
		resp.LastId = events[n-1].ID
	}

	return resp, nil
}

// marshalRPCChannelEvents converts a slice of chanevents.ChannelEvent into a
// slice of frdrpc.ChannelEvent.
func marshalRPCChannelEvents(
	events []*chanevents.ChannelEvent) []*frdrpc.ChannelEvent {

	rpcEvents := make([]*frdrpc.ChannelEvent, len(events))

	for i, event := range events {
		rpcEvent := &frdrpc.ChannelEvent{
			Id:        event.ID,
			Timestamp: event.Timestamp.Unix(),
			EventType: rpcEventType(event.EventType),
		}

		event.LocalBalance.WhenSome(
			func(b btcutil.Amount) {
				rpcEvent.LocalBalance = uint64(b)
			},
		)
		event.RemoteBalance.WhenSome(
			func(b btcutil.Amount) {
				rpcEvent.RemoteBalance = uint64(b)
			},
		)

		rpcEvents[i] = rpcEvent
	}

	return rpcEvents
}

// rpcEventType maps a stored chanevents.EventType to its proto counterpart.
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
