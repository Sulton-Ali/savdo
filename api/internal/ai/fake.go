package ai

import (
	"context"
	"sync"
)

// FakeResult is one scripted Chat outcome for Fake.
type FakeResult struct {
	Response Response
	Err      error
}

// Fake is a scripted Client for tests: it returns a fixed queue of
// FakeResult values in order, one per Chat call, and records every
// Request it was called with. Exported so callers outside this package
// (the bot's tool-loop tests, once built) can script and assert against
// it without a real provider.
type Fake struct {
	mu       sync.Mutex
	results  []FakeResult
	next     int
	Requests []Request
}

// NewFake builds a Fake that returns results in order, one per Chat call.
func NewFake(results ...FakeResult) *Fake {
	return &Fake{results: results}
}

// Chat implements Client. Calling it more times than there are scripted
// results is a test-authoring bug: it panics rather than silently
// returning a zero Response a test might mistake for a real answer.
func (f *Fake) Chat(_ context.Context, req Request) (Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.Requests = append(f.Requests, req)
	if f.next >= len(f.results) {
		panic("ai.Fake: Chat called more times than scripted results")
	}
	r := f.results[f.next]
	f.next++
	return r.Response, r.Err
}
