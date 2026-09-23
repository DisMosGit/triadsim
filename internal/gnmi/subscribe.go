package gnmi

import (
	"context"

	"github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/DisMosGit/triadsim/internal/datatree"
	"github.com/DisMosGit/triadsim/internal/event"
)

// Subscribe serves ONCE and STREAM/ON_CHANGE subscriptions. The subscription
// list of the first request is the one that creates the subscription.
func (s *service) Subscribe(stream grpc.BidiStreamingServer[gnmi.SubscribeRequest, gnmi.SubscribeResponse]) error {
	request, err := stream.Recv()
	if err != nil {
		return statusError(err)
	}

	list := request.GetSubscribe()
	if list == nil {
		return status.Error(codes.Unimplemented,
			"POLL is not supported; subscribe with mode ONCE or STREAM/ON_CHANGE")
	}

	encoding, err := encodingFor(list.GetEncoding())
	if err != nil {
		return err
	}
	paths, err := s.subscriptionPaths(list)
	if err != nil {
		return err
	}

	switch list.GetMode() {
	case gnmi.SubscriptionList_ONCE:
		return s.subscribeOnce(stream, paths, encoding)
	case gnmi.SubscriptionList_POLL:
		return status.Error(codes.Unimplemented, "POLL is not supported")
	default:
		return s.subscribeOnChange(stream, list, paths, encoding)
	}
}

// subscriptionPaths resolves the prefix and the subscription list into
// canonical router paths. An empty list subscribes to the prefix itself, which
// may be the model root.
func (s *service) subscriptionPaths(list *gnmi.SubscriptionList) ([]string, error) {
	prefix, err := routerPath(list.GetPrefix())
	if err != nil {
		return nil, err
	}

	subscriptions := list.GetSubscription()
	if len(subscriptions) == 0 {
		if err := checkPath(s.server.router, prefix); err != nil {
			return nil, err
		}
		return []string{prefix}, nil
	}

	paths := make([]string, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		if subscription.GetMode() != gnmi.SubscriptionMode_ON_CHANGE {
			return nil, status.Errorf(codes.Unimplemented,
				"subscription mode %s is not supported; use ON_CHANGE", subscription.GetMode())
		}
		// The Subscribe specification has an ON_CHANGE subscription re-send
		// unchanged values once per heartbeat_interval. The simulator does not
		// generate those re-notifications, so a non-zero interval is rejected
		// — explicitly, like POLL and SAMPLE — instead of being silently
		// ignored while the client waits for heartbeats that never come.
		if subscription.GetHeartbeatInterval() != 0 {
			return nil, status.Error(codes.Unimplemented,
				"heartbeat_interval is not supported; subscribe without it")
		}
		path, err := joinPrefix(prefix, subscription.GetPath())
		if err != nil {
			return nil, err
		}
		if err := checkPath(s.server.router, path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// subscribeOnce sends one snapshot per path and closes the stream.
func (s *service) subscribeOnce(stream grpc.BidiStreamingServer[gnmi.SubscribeRequest, gnmi.SubscribeResponse], paths []string, encoding gnmi.Encoding) error {
	for _, path := range paths {
		if err := s.sendSnapshot(stream.Context(), stream, path, encoding); err != nil {
			return err
		}
	}
	return sendSyncResponse(stream)
}

// subscribeOnChange sends the initial snapshot, then one notification per
// matching EventBus event until the client goes away.
func (s *service) subscribeOnChange(stream grpc.BidiStreamingServer[gnmi.SubscribeRequest, gnmi.SubscribeResponse], list *gnmi.SubscriptionList, paths []string, encoding gnmi.Encoding) error {
	if s.server.bus == nil {
		return status.Error(codes.Unimplemented,
			"STREAM subscriptions need the event bus, which is not configured")
	}

	if !list.GetUpdatesOnly() {
		for _, path := range paths {
			if err := s.sendSnapshot(stream.Context(), stream, path, encoding); err != nil {
				return err
			}
		}
	}
	if err := sendSyncResponse(stream); err != nil {
		return err
	}

	events := s.server.bus.Subscribe()
	defer s.server.bus.Unsubscribe(events)

	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case busEvent, ok := <-events:
			if !ok {
				return nil
			}
			if err := s.sendEvent(ctx, stream, paths, busEvent, encoding); err != nil {
				return err
			}
		}
	}
}

// sendEvent notifies every subscription the event concerns. One event may touch
// several paths (a radio link alarm and a sync transition are separate events,
// but a configuration change concerns the root), and every matching
// subscription is notified once per target.
func (s *service) sendEvent(ctx context.Context, stream grpc.BidiStreamingServer[gnmi.SubscribeRequest, gnmi.SubscribeResponse], paths []string, busEvent event.Event, encoding gnmi.Encoding) error {
	for _, affected := range affectedPaths(busEvent) {
		notified := make(map[string]bool, len(paths))
		for _, subscription := range paths {
			if !matchesSubscription(subscription, affected) {
				continue
			}
			target := notificationTarget(subscription, affected)
			if notified[target] {
				continue
			}
			notified[target] = true

			notification, err := s.notification(ctx, target, datatree.ContentAll, encoding)
			if err != nil {
				return err
			}
			if notification == nil {
				continue
			}
			if err := stream.Send(&gnmi.SubscribeResponse{
				Response: &gnmi.SubscribeResponse_Update{Update: notification},
			}); err != nil {
				return statusError(err)
			}
		}
	}
	return nil
}

// sendSnapshot sends the current data of one path.
func (s *service) sendSnapshot(ctx context.Context, stream grpc.BidiStreamingServer[gnmi.SubscribeRequest, gnmi.SubscribeResponse], path string, encoding gnmi.Encoding) error {
	notification, err := s.notification(ctx, path, datatree.ContentAll, encoding)
	if err != nil {
		return err
	}
	if notification == nil {
		return nil
	}
	if err := stream.Send(&gnmi.SubscribeResponse{
		Response: &gnmi.SubscribeResponse_Update{Update: notification},
	}); err != nil {
		return statusError(err)
	}
	return nil
}

// sendSyncResponse marks the end of the initial snapshot.
func sendSyncResponse(stream grpc.BidiStreamingServer[gnmi.SubscribeRequest, gnmi.SubscribeResponse]) error {
	if err := stream.Send(&gnmi.SubscribeResponse{
		Response: &gnmi.SubscribeResponse_SyncResponse{SyncResponse: true},
	}); err != nil {
		return statusError(err)
	}
	return nil
}
