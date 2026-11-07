// Package messagebus is an in-process, best-effort pub/sub carrier for bare
// per-owner ListChanged signals.
package messagebus

import (
	"context"
	"errors"
	"sync"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// ownerState is one owner's change counter plus a broadcast channel that is
// closed (and replaced) on every publish to wake all waiting subscribers.
type ownerState struct {
	seq  int64
	wake chan struct{}
}

type messageBus struct {
	mu     sync.Mutex
	owners map[string]*ownerState
}

func newMessageBus() MessageBus {
	return &messageBus{owners: make(map[string]*ownerState)}
}

func (b *messageBus) state(owner string) *ownerState {
	s, ok := b.owners[owner]
	if !ok {
		s = &ownerState{wake: make(chan struct{})}
		b.owners[owner] = s
	}
	return s
}

// Publish is total and non-blocking; an empty owner is a silent no-op.
func (b *messageBus) Publish(_ fwra.Context, event ListChanged) {
	if event.Owner == "" {
		return
	}
	b.mu.Lock()
	s := b.state(event.Owner)
	s.seq++
	close(s.wake)
	s.wake = make(chan struct{})
	b.mu.Unlock()
}

// Subscribe returns at once when the owner's sequence exceeds afterSequence,
// otherwise blocks until the next publish or the caller's context ends.
func (b *messageBus) Subscribe(rc fwra.Context, owner string, afterSequence int64) (ListChangedNotice, error) {
	if owner == "" {
		return ListChangedNotice{}, errors.New("messagebus: invalid argument: owner is empty")
	}
	if afterSequence < 0 {
		return ListChangedNotice{}, errors.New("messagebus: invalid argument: afterSequence is negative")
	}
	ctx := rc.Context
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		b.mu.Lock()
		s := b.state(owner)
		seq, wake := s.seq, s.wake
		b.mu.Unlock()
		if seq > afterSequence {
			return ListChangedNotice{Owner: owner, Sequence: seq}, nil
		}
		select {
		case <-wake:
		case <-ctx.Done():
			return ListChangedNotice{}, ctx.Err()
		}
	}
}
