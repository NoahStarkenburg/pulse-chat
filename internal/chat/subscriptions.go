package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/NoahStarkenburg/pulse-chat/internal/bus"
)

// channelForRoom is the Pub/Sub channel a room's messages flow through. The room
// name maps 1:1 to a channel, prefixed so room channels are namespaced apart
// from any other use of the bus.
func channelForRoom(room string) string { return "room:" + room }

// subscribeTimeout bounds establishing a subscription so a slow or unreachable
// Redis fails the acquire instead of hanging the joining connection.
const subscribeTimeout = 5 * time.Second

// subscriptions manages one bus subscription per room that has local clients,
// reference-counted: the first local client in a room subscribes, the last to
// leave unsubscribes. Each subscription runs a pump goroutine that decodes
// incoming messages and hands them to deliver for local fan-out.
//
// This lives off the Hub's single goroutine on purpose: subscribing does network
// I/O, and the Hub must never block on the network. Connections call acquire and
// release from their own goroutines.
type subscriptions struct {
	bus     bus.PubSub
	deliver func(Envelope) // hand a decoded message to the Hub for local fan-out
	logger  *slog.Logger

	mu    sync.Mutex
	rooms map[string]*roomSub
}

type roomSub struct {
	refs      int
	sub       bus.Subscription
	done      chan struct{}
	closeOnce sync.Once
}

func (rs *roomSub) shutdown() {
	rs.closeOnce.Do(func() {
		close(rs.done)
		_ = rs.sub.Close()
	})
}

func newSubscriptions(b bus.PubSub, deliver func(Envelope), logger *slog.Logger) *subscriptions {
	return &subscriptions{
		bus:     b,
		deliver: deliver,
		logger:  logger,
		rooms:   make(map[string]*roomSub),
	}
}

// acquire ensures this instance is subscribed to room and increments the
// reference count. The first acquirer subscribes (synchronously, so a publish
// that immediately follows the join is delivered back) and starts the pump.
func (s *subscriptions) acquire(room string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if rs := s.rooms[room]; rs != nil {
		rs.refs++
		return nil
	}

	// A fresh, bounded context: the subscription must outlive any single
	// connection, so it is not tied to a request context, and go-redis receives
	// messages on its own internal context once subscribed.
	ctx, cancel := context.WithTimeout(context.Background(), subscribeTimeout)
	defer cancel()

	sub, err := s.bus.Subscribe(ctx, channelForRoom(room))
	if err != nil {
		return err
	}
	rs := &roomSub{refs: 1, sub: sub, done: make(chan struct{})}
	s.rooms[room] = rs
	go s.pump(room, rs)
	return nil
}

// release decrements the reference count and, when it reaches zero, closes the
// subscription so no instance holds a room channel open with no local clients.
func (s *subscriptions) release(room string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rs := s.rooms[room]
	if rs == nil {
		return
	}
	rs.refs--
	if rs.refs > 0 {
		return
	}
	delete(s.rooms, room)
	rs.shutdown()
}

// pump decodes each message from the room's subscription and delivers it to the
// local Hub. It exits when the subscription is closed (Messages closes) or the
// room is released (done closes).
func (s *subscriptions) pump(room string, rs *roomSub) {
	for {
		select {
		case data, ok := <-rs.sub.Messages():
			if !ok {
				return
			}
			var env Envelope
			if err := json.Unmarshal(data, &env); err != nil {
				s.logger.Warn("dropping malformed pub/sub message", "room", room, "err", err)
				continue
			}
			s.deliver(env)
		case <-rs.done:
			return
		}
	}
}

// closeAll closes every active subscription. Called on Hub shutdown.
func (s *subscriptions) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for room, rs := range s.rooms {
		rs.shutdown()
		delete(s.rooms, room)
	}
}
