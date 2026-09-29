// Package progress records target-acknowledged source replication positions.
// These positions are observational; they are not checkpoints for resuming PSYNC.
package progress

import (
	"container/list"
	"context"
	"sort"
	"sync"
)

type Position struct {
	Node   string `json:"node"`
	Offset int64  `json:"offset,string"` // Preserve int64 precision in browsers.
}

type pending struct {
	offset int64
	done   bool
}

type source struct {
	queue  list.List
	offset int64
	known  bool
	replID string
}

type Tracker struct {
	mu      sync.Mutex
	sources map[string]*source
	slots   chan struct{}
}

func NewTracker(limit int) *Tracker {
	return &Tracker{sources: make(map[string]*source), slots: make(chan struct{}, limit)}
}

// SetReplicationID binds positions to the history returned by FULLRESYNC.
func (t *Tracker) SetReplicationID(node, id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sources[node] == nil {
		t.sources[node] = &source{}
	}
	t.sources[node].replID = id
}

// Track registers commands in source order. Completion can arrive in any order
// from target shards, but the visible position only passes a completed prefix.
// The bounded window applies backpressure when one shard falls behind.
func (t *Tracker) Track(ctx context.Context, node string, offset int64) (func(), error) {
	select {
	case t.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	t.mu.Lock()
	s := t.sources[node]
	if s == nil {
		s = &source{}
		t.sources[node] = s
	}
	p := &pending{offset: offset}
	s.queue.PushBack(p)
	t.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			p.done = true
			for head := s.queue.Front(); head != nil; head = s.queue.Front() {
				item := head.Value.(*pending)
				if !item.done {
					break
				}
				s.offset, s.known = item.offset, true
				s.queue.Remove(head)
				<-t.slots
			}
		})
	}, nil
}

func (t *Tracker) Snapshot() []Position {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Position, 0, len(t.sources))
	for node, s := range t.sources {
		if s.known {
			out = append(out, Position{Node: node, Offset: s.offset})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Node < out[j].Node })
	return out
}
