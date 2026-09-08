package runtime

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/r6m/tlrpc/mtproto"
)

func TestRecoveryPushBarrierAcceptsAndDrainsAfterReply(t *testing.T) {
	var (
		mu    sync.Mutex
		order []string
	)
	barrier := newRecoveryPushBarrier(context.Background(), 4, 64, func(_ context.Context, intent Intent) error {
		push := intent.(Push)
		mu.Lock()
		order = append(order, string(push.Body))
		mu.Unlock()
		return nil
	}, nil)
	token := barrier.begin()

	body := []byte("first")
	if err := barrier.push(context.Background(), body); err != nil {
		t.Fatalf("accept queued push: %v", err)
	}
	body[0] = 'X'
	mu.Lock()
	if len(order) != 0 {
		t.Fatalf("push submitted before reply: %v", order)
	}
	mu.Unlock()

	if err := token.finish(func() error {
		mu.Lock()
		order = append(order, "reply")
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatalf("finish barrier: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(order, []string{"reply", "first"}) {
		t.Fatalf("submission order = %v, want reply then copied push", order)
	}
}

func TestRecoveryPushBarrierAllowsConcurrentInactivePushesBeforeProtectedBegin(t *testing.T) {
	started := make(chan string, 3)
	release := make(chan struct{})
	barrier := newRecoveryPushBarrier(context.Background(), 4, 64, func(_ context.Context, intent Intent) error {
		value := string(intent.(Push).Body)
		started <- value
		if value != "third" {
			<-release
		}
		return nil
	}, nil)

	pushes := make(chan error, 3)
	go func() { pushes <- barrier.push(context.Background(), []byte("first")) }()
	go func() { pushes <- barrier.push(context.Background(), []byte("second")) }()
	seen := map[string]bool{}
	for len(seen) != 2 {
		select {
		case value := <-started:
			seen[value] = true
		case <-time.After(time.Second):
			t.Fatal("inactive pushes did not submit concurrently")
		}
	}

	tokenReady := make(chan *recoveryPushBarrierToken, 1)
	go func() { tokenReady <- barrier.begin() }()
	// Give the exclusive begin call a chance to wait behind both in-flight
	// readers. sync.RWMutex then prevents a newer direct push from passing it.
	time.Sleep(20 * time.Millisecond)
	go func() { pushes <- barrier.push(context.Background(), []byte("third")) }()
	select {
	case <-pushes:
		t.Fatal("newer push passed a waiting protected begin")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	for index := 0; index < 2; index++ {
		if err := <-pushes; err != nil {
			t.Fatal(err)
		}
	}
	var token *recoveryPushBarrierToken
	select {
	case token = <-tokenReady:
	case <-time.After(time.Second):
		t.Fatal("protected begin did not resume after direct pushes")
	}
	if err := <-pushes; err != nil {
		t.Fatal(err)
	}
	select {
	case value := <-started:
		t.Fatalf("queued push submitted before protected reply: %q", value)
	default:
	}
	if err := token.finish(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	select {
	case value := <-started:
		if value != "third" {
			t.Fatalf("drained push = %q, want third", value)
		}
	case <-time.After(time.Second):
		t.Fatal("queued push did not drain after protected reply")
	}
}

func TestRecoveryPushBarrierSerializesNewProtectedRequestBehindDrain(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var (
		mu    sync.Mutex
		order []string
	)
	barrier := newRecoveryPushBarrier(context.Background(), 4, 64, func(_ context.Context, intent Intent) error {
		value := string(intent.(Push).Body)
		if value == "first" {
			close(firstStarted)
			<-releaseFirst
		}
		mu.Lock()
		order = append(order, value)
		mu.Unlock()
		return nil
	}, nil)
	token := barrier.begin()
	if err := barrier.push(context.Background(), []byte("first")); err != nil {
		t.Fatalf("queue first: %v", err)
	}
	finished := make(chan error, 1)
	go func() { finished <- token.finish(func() error { return nil }) }()
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("drain did not start")
	}

	secondToken := make(chan *recoveryPushBarrierToken, 1)
	beginStarted := make(chan struct{})
	go func() {
		close(beginStarted)
		secondToken <- barrier.begin()
	}()
	<-beginStarted
	select {
	case <-secondToken:
		t.Fatal("new protected request began during an in-flight drain submission")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseFirst)
	if err := <-finished; err != nil {
		t.Fatalf("finish barrier: %v", err)
	}
	token2 := <-secondToken
	if err := barrier.push(context.Background(), []byte("second")); err != nil {
		t.Fatalf("queue second: %v", err)
	}
	if err := token2.finish(func() error {
		mu.Lock()
		order = append(order, "reply2")
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatalf("finish second barrier: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(order, []string{"first", "reply2", "second"}) {
		t.Fatalf("drain order = %v, want old push then newer reply and push", order)
	}
}

func TestRecoveryPushBarrierAcceptsNewPushDuringDrain(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var (
		mu    sync.Mutex
		order []string
	)
	barrier := newRecoveryPushBarrier(context.Background(), 4, 64, func(_ context.Context, intent Intent) error {
		value := string(intent.(Push).Body)
		if value == "first" {
			close(firstStarted)
			<-releaseFirst
		}
		mu.Lock()
		order = append(order, value)
		mu.Unlock()
		return nil
	}, nil)
	token := barrier.begin()
	if err := barrier.push(context.Background(), []byte("first")); err != nil {
		t.Fatalf("queue first: %v", err)
	}
	finished := make(chan error, 1)
	go func() { finished <- token.finish(func() error { return nil }) }()
	<-firstStarted
	if err := barrier.push(context.Background(), []byte("second")); err != nil {
		t.Fatalf("accept push during drain: %v", err)
	}
	close(releaseFirst)
	if err := <-finished; err != nil {
		t.Fatalf("finish: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(order, []string{"first", "second"}) {
		t.Fatalf("drain order = %v, want FIFO", order)
	}
}

func TestRecoveryPushBarrierOverflowRetiresSession(t *testing.T) {
	tests := []struct {
		name     string
		count    int
		bytes    int
		first    []byte
		overflow []byte
	}{
		{name: "count", count: 1, bytes: 64, first: []byte("a"), overflow: []byte("b")},
		{name: "bytes", count: 4, bytes: 3, first: []byte("ab"), overflow: []byte("cd")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			retired := make(chan error, 1)
			barrier := newRecoveryPushBarrier(context.Background(), test.count, test.bytes, func(context.Context, Intent) error {
				t.Fatal("overflowed barrier submitted a push")
				return nil
			}, func(err error) { retired <- err })
			_ = barrier.begin()
			if err := barrier.push(context.Background(), test.first); err != nil {
				t.Fatalf("accept first push: %v", err)
			}
			err := barrier.push(context.Background(), test.overflow)
			if !errors.Is(err, ErrRecoveryPushBarrierFull) {
				t.Fatalf("overflow error = %v, want ErrRecoveryPushBarrierFull", err)
			}
			select {
			case retireErr := <-retired:
				if !errors.Is(retireErr, ErrRecoveryPushBarrierFull) {
					t.Fatalf("retire error = %v", retireErr)
				}
			case <-time.After(time.Second):
				t.Fatal("overflow did not retire session")
			}
		})
	}
}

func TestRecoveryPushBarrierFailedReplyDoesNotFlushAndRetires(t *testing.T) {
	var submitted int
	retired := make(chan error, 1)
	barrier := newRecoveryPushBarrier(context.Background(), 2, 16, func(_ context.Context, intent Intent) error {
		submitted++
		return nil
	}, func(err error) { retired <- err })
	token := barrier.begin()
	if err := barrier.push(context.Background(), []byte("queued")); err != nil {
		t.Fatalf("queue push: %v", err)
	}
	replyErr := context.Canceled
	if err := token.finish(func() error { return replyErr }); !errors.Is(err, replyErr) {
		t.Fatalf("finish error = %v, want %v", err, replyErr)
	}
	if submitted != 0 {
		t.Fatalf("submitted pushes = %d, want no flush behind failed reply", submitted)
	}
	select {
	case err := <-retired:
		if !errors.Is(err, replyErr) {
			t.Fatalf("retire error = %v, want %v", err, replyErr)
		}
	case <-time.After(time.Second):
		t.Fatal("failed reply did not retire session")
	}
}

func TestRecoveryPushBarrierCanceledPushIsNotQueued(t *testing.T) {
	barrier := newRecoveryPushBarrier(context.Background(), 1, 1, func(context.Context, Intent) error {
		t.Fatal("canceled push was submitted")
		return nil
	}, nil)
	token := barrier.begin()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := barrier.push(ctx, []byte("too large for queue")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled push error = %v, want context canceled", err)
	}
	barrier.close(context.Canceled)
	token.cancel()
}

func TestRecoveryPushBarrierCloseDiscardsQueuedBodies(t *testing.T) {
	barrier := newRecoveryPushBarrier(context.Background(), 2, 16, func(context.Context, Intent) error {
		t.Fatal("closed barrier submitted queued push")
		return nil
	}, nil)
	token := barrier.begin()
	if err := barrier.push(context.Background(), []byte("stale")); err != nil {
		t.Fatalf("queue push: %v", err)
	}
	cause := errors.New("lease replaced")
	barrier.close(cause)
	token.cancel()
	if err := barrier.push(context.Background(), []byte("later")); !errors.Is(err, cause) {
		t.Fatalf("push after close = %v, want lease cause", err)
	}
}

func TestRecoveryPushBarrierPushAfterCloseWithActiveTokenFails(t *testing.T) {
	barrier := newRecoveryPushBarrier(context.Background(), 2, 16, func(context.Context, Intent) error {
		t.Fatal("closed barrier submitted push")
		return nil
	}, nil)
	token := barrier.begin()
	cause := errors.New("session retired")
	barrier.close(cause)
	if err := barrier.push(context.Background(), []byte("stale")); !errors.Is(err, cause) {
		t.Fatalf("push after close = %v, want %v", err, cause)
	}
	token.cancel()
}

func TestConnectionSessionShutdownCancelsBlockedBarrierDrainBeforeClose(t *testing.T) {
	now := time.Unix(inboundNowSeconds, 0).UTC()
	harness := newConnectionHarness(t, now, &connectionApplicationStub{}, 10, nil)
	actor, err := harness.connection.sessionFor(context.Background(), DecodedFrame{
		Encrypted: &mtproto.InnerData{Salt: inboundSalt, SessionID: inboundSessionID},
		AuthKeyID: harness.authKey.ID(), AuthKey: harness.authKey,
	})
	if err != nil {
		t.Fatalf("create connection session: %v", err)
	}

	submitStarted := make(chan struct{})
	actor.pushBarrier = newRecoveryPushBarrier(actor.lease.Context(), 2, 16, func(ctx context.Context, intent Intent) error {
		if _, ok := intent.(Push); !ok {
			t.Fatalf("submitted intent = %T, want Push", intent)
		}
		close(submitStarted)
		<-ctx.Done()
		return context.Cause(ctx)
	}, actor.lease.Retire)
	token := actor.pushBarrier.begin()
	if err := actor.pushBarrier.push(context.Background(), []byte("queued")); err != nil {
		t.Fatalf("queue push: %v", err)
	}
	finishDone := make(chan error, 1)
	go func() { finishDone <- token.finish(func() error { return nil }) }()
	select {
	case <-submitStarted:
	case <-time.After(time.Second):
		t.Fatal("barrier drain did not block in submit")
	}

	shutdownCause := errors.New("test shutdown")
	shutdownDone := make(chan struct{})
	go func() {
		actor.shutdown(shutdownCause)
		close(shutdownDone)
	}()
	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("session shutdown deadlocked behind blocked barrier drain")
	}
	if finishErr := <-finishDone; !errors.Is(finishErr, shutdownCause) {
		t.Fatalf("drain error = %v, want shutdown cause", finishErr)
	}
	if err := actor.pushBarrier.push(context.Background(), []byte("stale")); !errors.Is(err, shutdownCause) {
		t.Fatalf("push after shutdown = %v, want shutdown cause", err)
	}
}
