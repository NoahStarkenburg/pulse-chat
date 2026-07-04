package bus

import (
	"context"
	"fmt"
	"sync"

	"github.com/redis/go-redis/v9"
)

// RedisPubSub is a PubSub backed by Redis PUBLISH/SUBSCRIBE.
type RedisPubSub struct {
	client *redis.Client
}

// NewRedisPubSub connects to Redis from a redis:// URL and verifies the
// connection with a PING, so a bad URL or unreachable Redis fails fast at
// startup rather than on the first publish. The caller owns the returned bus and
// must Close it on shutdown.
func NewRedisPubSub(ctx context.Context, url string) (*RedisPubSub, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parsing redis url: %w", err)
	}
	client := redis.NewClient(opt)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("pinging redis: %w", err)
	}
	return &RedisPubSub{client: client}, nil
}

// Ping reports whether Redis is reachable. Used by the readiness check.
func (r *RedisPubSub) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

func (r *RedisPubSub) Publish(ctx context.Context, channel string, payload []byte) error {
	return r.client.Publish(ctx, channel, payload).Err()
}

// Subscribe subscribes to channel. go-redis subscribes lazily, so we Receive
// once to confirm the subscription is established and surface a connection error
// now rather than silently dropping messages later.
func (r *RedisPubSub) Subscribe(ctx context.Context, channel string) (Subscription, error) {
	ps := r.client.Subscribe(ctx, channel)
	if _, err := ps.Receive(ctx); err != nil {
		_ = ps.Close()
		return nil, fmt.Errorf("subscribing to %q: %w", channel, err)
	}
	return newRedisSubscription(ps), nil
}

func (r *RedisPubSub) Close() error {
	return r.client.Close()
}

// redisSubscription adapts a *redis.PubSub to the Subscription interface,
// translating *redis.Message into the raw []byte payload the bus speaks.
type redisSubscription struct {
	ps   *redis.PubSub
	out  chan []byte
	done chan struct{}
	once sync.Once
}

func newRedisSubscription(ps *redis.PubSub) *redisSubscription {
	s := &redisSubscription{
		ps:   ps,
		out:  make(chan []byte, 64),
		done: make(chan struct{}),
	}
	go s.pump()
	return s
}

// pump copies payloads from go-redis's message channel onto out until the
// subscription is closed. The inner select lets a Close unblock a send that a
// stalled consumer would otherwise wedge.
func (s *redisSubscription) pump() {
	defer close(s.out)
	ch := s.ps.Channel()
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			select {
			case s.out <- []byte(msg.Payload):
			case <-s.done:
				return
			}
		case <-s.done:
			return
		}
	}
}

func (s *redisSubscription) Messages() <-chan []byte { return s.out }

func (s *redisSubscription) Close() error {
	var err error
	s.once.Do(func() {
		close(s.done)
		err = s.ps.Close()
	})
	return err
}
