package bus

import (
	"context"
	"sync"
)

// Memory is an in-process PubSub. It implements the same fire-and-forget
// semantics as the Redis bus, so two in-process "instances" that share one
// Memory stand in for two servers sharing one Redis. Tests use it to exercise
// cross-instance delivery with no broker running.
type Memory struct {
	mu   sync.Mutex
	subs map[string]map[*memorySubscription]struct{}
}

// NewMemory returns an empty in-memory bus.
func NewMemory() *Memory {
	return &Memory{subs: make(map[string]map[*memorySubscription]struct{})}
}

func (m *Memory) Publish(_ context.Context, channel string, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for s := range m.subs[channel] {
		// Copy so subscribers cannot see each other's mutations, and send
		// non-blocking: a subscriber that is not keeping up drops the message,
		// matching Pub/Sub fire-and-forget.
		cp := make([]byte, len(payload))
		copy(cp, payload)
		select {
		case s.out <- cp:
		default:
		}
	}
	return nil
}

func (m *Memory) Subscribe(_ context.Context, channel string) (Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &memorySubscription{bus: m, channel: channel, out: make(chan []byte, 64)}
	set := m.subs[channel]
	if set == nil {
		set = make(map[*memorySubscription]struct{})
		m.subs[channel] = set
	}
	set[s] = struct{}{}
	return s, nil
}

func (m *Memory) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ch, set := range m.subs {
		for s := range set {
			s.once.Do(func() { close(s.out) })
		}
		delete(m.subs, ch)
	}
	return nil
}

type memorySubscription struct {
	bus     *Memory
	channel string
	out     chan []byte
	once    sync.Once
}

func (s *memorySubscription) Messages() <-chan []byte { return s.out }

func (s *memorySubscription) Close() error {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	s.once.Do(func() {
		if set := s.bus.subs[s.channel]; set != nil {
			delete(set, s)
			if len(set) == 0 {
				delete(s.bus.subs, s.channel)
			}
		}
		close(s.out)
	})
	return nil
}
