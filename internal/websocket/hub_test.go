package websocket

import (
	"context"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type fakeHubPubSub struct {
	mu            sync.Mutex
	channels      map[string]bool
	onSubscribe   func(context.Context, []string) error
	onUnsubscribe func(context.Context, []string) error
}

func (p *fakeHubPubSub) Subscribe(ctx context.Context, channels ...string) error {
	var err error
	if p.onSubscribe != nil {
		err = p.onSubscribe(ctx, channels)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	// go-redis remembers requested channels even when Subscribe returns an error.
	for _, ch := range channels {
		p.channels[ch] = true
	}
	return err
}

func (p *fakeHubPubSub) Unsubscribe(ctx context.Context, channels ...string) error {
	if p.onUnsubscribe != nil {
		if err := p.onUnsubscribe(ctx, channels); err != nil {
			return err
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, ch := range channels {
		delete(p.channels, ch)
	}
	return nil
}

func (p *fakeHubPubSub) Channel(...redis.ChannelOption) <-chan *redis.Message { return nil }
func (p *fakeHubPubSub) Close() error                                         { return nil }

func newSubscriptionTestHub() (*Hub, *fakeHubPubSub) {
	p := &fakeHubPubSub{channels: make(map[string]bool)}
	h := &Hub{
		ctx: context.Background(), done: make(chan struct{}),
		clients:  make(map[string]map[string]*Client),
		channels: make(map[string]map[string]struct{}),
		pubsub:   p, rm: NewRoomManager(nil), log: zap.NewNop(),
	}
	return h, p
}

func subscriptionTestID(last byte) []byte {
	id := make([]byte, 16)
	id[15] = last
	return id
}

func subscriptionTestClient(h *Hub, uid byte, convs [][]byte) *Client {
	return NewClient(subscriptionTestID(uid), nil, h, h.rm, nil, nil, convs, h.log)
}

func waitSubscriptionResult(t *testing.T, result <-chan bool) bool {
	t.Helper()
	select {
	case ok := <-result:
		return ok
	case <-time.After(5 * time.Second):
		t.Fatal("subscription operation did not complete")
		return false
	}
}

func assertSubscriptionDelivery(t *testing.T, h *Hub, p *fakeHubPubSub, client *Client, ch string) {
	t.Helper()
	p.mu.Lock()
	subscribed := p.channels[ch]
	p.mu.Unlock()
	if !subscribed {
		t.Fatalf("active client lost Redis subscription to %s", ch)
	}
	payload := []byte(`{"event":"test"}`)
	h.dispatchToClients(ch, payload)
	select {
	case got, ok := <-client.send:
		if !ok || string(got) != string(payload) {
			t.Fatalf("unexpected delivery on %s: %q, open=%v", ch, got, ok)
		}
	default:
		t.Fatalf("active client did not receive message on %s", ch)
	}
}

func TestRegisterWaitsForFailedSubscriptionRollback(t *testing.T) {
	for _, sameUser := range []bool{true, false} {
		name := "different users"
		if sameUser {
			name = "same user"
		}
		t.Run(name, func(t *testing.T) {
			h, p := newSubscriptionTestHub()
			started, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			var callMu sync.Mutex
			calls := 0
			p.onSubscribe = func(ctx context.Context, _ []string) error {
				callMu.Lock()
				calls++
				first := calls == 1
				callMu.Unlock()
				if !first {
					return nil
				}
				close(started)
				select {
				case <-release:
					return errors.New("injected Redis subscription failure")
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			convs := [][]byte{subscriptionTestID(9)}
			first := subscriptionTestClient(h, 1, convs)
			secondUID := byte(2)
			if sameUser {
				secondUID = 1
			}
			second := subscriptionTestClient(h, secondUID, convs)
			firstResult := make(chan bool, 1)
			go func() { firstResult <- h.Register(first, convs) }()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("first subscription never started")
			}
			secondResult := make(chan bool, 1)
			go func() { secondResult <- h.Register(second, convs) }()
			select {
			case <-secondResult:
				t.Fatal("registration completed before the shared subscription was resolved")
			case <-time.After(25 * time.Millisecond):
			}
			unblock()
			if waitSubscriptionResult(t, firstResult) {
				t.Fatal("failed subscription was accepted")
			}
			if !waitSubscriptionResult(t, secondResult) {
				t.Fatal("registration after rollback failed")
			}
			assertSubscriptionDelivery(t, h, p, second, notifyChannel(convs[0]))
			assertSubscriptionDelivery(t, h, p, second, sysChannel(second.uidHex))
		})
	}
}

func TestRegisterWaitsForPendingUnsubscribe(t *testing.T) {
	h, p := newSubscriptionTestHub()
	convs := [][]byte{subscriptionTestID(9)}
	first := subscriptionTestClient(h, 1, convs)
	second := subscriptionTestClient(h, 1, convs)
	if !h.Register(first, convs) {
		t.Fatal("initial registration failed")
	}
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	p.onUnsubscribe = func(ctx context.Context, _ []string) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	unregistered := make(chan bool, 1)
	go func() { h.Unregister(first); unregistered <- true }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("unsubscribe never started")
	}
	registered := make(chan bool, 1)
	go func() { registered <- h.Register(second, convs) }()
	select {
	case <-registered:
		t.Fatal("registration overtook an unfinished unsubscribe")
	case <-time.After(25 * time.Millisecond):
	}
	unblock()
	waitSubscriptionResult(t, unregistered)
	if !waitSubscriptionResult(t, registered) {
		t.Fatal("reconnection failed")
	}
	assertSubscriptionDelivery(t, h, p, second, notifyChannel(convs[0]))
	assertSubscriptionDelivery(t, h, p, second, sysChannel(second.uidHex))
}

func TestUnregisterPreservesOtherSessionsAndIgnoresStaleClient(t *testing.T) {
	h, p := newSubscriptionTestHub()
	convs := [][]byte{subscriptionTestID(9)}
	first := subscriptionTestClient(h, 1, convs)
	second := subscriptionTestClient(h, 1, convs)
	if !h.Register(first, convs) || !h.Register(second, convs) {
		t.Fatal("registration failed")
	}
	h.Unregister(first)
	h.Unregister(first)
	h.unsubscribeLocalClient(first, convs[0])
	if h.subscribeLocalClient(first, subscriptionTestID(10)) {
		t.Fatal("stale client was allowed to subscribe")
	}
	assertSubscriptionDelivery(t, h, p, second, notifyChannel(convs[0]))
	assertSubscriptionDelivery(t, h, p, second, sysChannel(second.uidHex))
	h.Unregister(second)
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.channels) != 0 || len(h.channels) != 0 || len(h.clients) != 0 {
		t.Fatal("subscriptions remained after the last session disconnected")
	}
}

func TestFailedConversationSubscriptionIsCleanedUpAndCanRetry(t *testing.T) {
	h, p := newSubscriptionTestHub()
	client := subscriptionTestClient(h, 1, nil)
	if !h.Register(client, nil) {
		t.Fatal("initial registration failed")
	}
	conv := subscriptionTestID(9)
	p.onSubscribe = func(context.Context, []string) error { return errors.New("injected failure") }
	if h.subscribeLocalClient(client, conv) {
		t.Fatal("failed subscription was accepted")
	}
	if client.hasConv(hex.EncodeToString(conv)) {
		t.Fatal("failed conversation was added to client")
	}
	p.mu.Lock()
	remembered := p.channels[notifyChannel(conv)]
	p.mu.Unlock()
	if remembered {
		t.Fatal("failed conversation remained registered for Redis reconnection")
	}
	p.onSubscribe = nil
	if !h.subscribeLocalClient(client, conv) {
		t.Fatal("retry failed")
	}
	assertSubscriptionDelivery(t, h, p, client, notifyChannel(conv))
}
