package web

import "sync"

// Bus tells every open page of one user that something of theirs changed.
//
// The notification carries NOTHING but the fact of a change. Each subscriber
// re-reads its own data and renders it itself, so two changes racing each other
// both end in the current truth — whereas pushing rendered HTML would let an
// older render arrive last and win.
//
// ⚠ There is deliberately no NATS here, a departure from the house CQRS pattern
// that is stated rather than left to be discovered. This is one binary with one
// database file; a cross-process notification spine would be a service to run
// and secure in exchange for nothing, because there is no second process to
// notify. Broadcast is where a publish would go, and no handler would change.
type Bus struct {
	mu   sync.Mutex
	subs map[int64]map[chan struct{}]struct{}
}

// NewBus returns an empty bus.
func NewBus() *Bus {
	return &Bus{subs: make(map[int64]map[chan struct{}]struct{})}
}

// Subscribe returns a channel that receives a signal whenever userID's data
// changes, and a function that unsubscribes it.
//
// The channel holds one pending signal and Broadcast drops rather than blocks,
// so a slow page can never stall a writer. Dropping is free because the signal
// carries no data: the reader re-reads on the next one and sees everything it
// missed.
func (b *Bus) Subscribe(userID int64) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)

	b.mu.Lock()
	if b.subs[userID] == nil {
		b.subs[userID] = make(map[chan struct{}]struct{})
	}
	b.subs[userID][ch] = struct{}{}
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if set, ok := b.subs[userID]; ok {
			delete(set, ch)
			// Drop the user's entry once nobody is listening, so the map does not
			// grow by one key per user who ever opened a page.
			if len(set) == 0 {
				delete(b.subs, userID)
			}
		}
	}
}

// Broadcast signals every open page of userID.
//
// It is called AFTER a write has been persisted, never before: announcing first
// would let a page show a change that then failed to save.
func (b *Bus) Broadcast(userID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[userID] {
		select {
		case ch <- struct{}{}:
		default:
			// A signal is already pending; a second tells the reader nothing new.
		}
	}
}
