package ai

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

func TestFake_returnsScriptedResultsInOrderAndRecordsRequests(t *testing.T) {
	wantErr := errors.New("boom")
	fake := NewFake(
		FakeResult{Response: Response{Text: "first"}},
		FakeResult{Err: wantErr},
	)

	req1 := Request{System: "sys1", Messages: []Message{{Role: RoleUser, Text: "hi"}}}
	resp1, err1 := fake.Chat(context.Background(), req1)
	if err1 != nil {
		t.Fatalf("Chat #1 error = %v, want nil", err1)
	}
	if resp1.Text != "first" {
		t.Fatalf("Chat #1 Text = %q, want %q", resp1.Text, "first")
	}

	req2 := Request{System: "sys2"}
	_, err2 := fake.Chat(context.Background(), req2)
	if !errors.Is(err2, wantErr) {
		t.Fatalf("Chat #2 error = %v, want %v", err2, wantErr)
	}

	if len(fake.Requests) != 2 {
		t.Fatalf("Requests recorded = %d, want 2", len(fake.Requests))
	}
	if fake.Requests[0].System != "sys1" || fake.Requests[1].System != "sys2" {
		t.Fatalf("Requests = %+v, want [sys1, sys2]", fake.Requests)
	}
}

func TestFake_panicsWhenScriptIsExhausted(t *testing.T) {
	fake := NewFake(FakeResult{Response: Response{Text: "only one"}})

	if _, err := fake.Chat(context.Background(), Request{}); err != nil {
		t.Fatalf("Chat #1 error = %v, want nil", err)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("Chat #2 did not panic, want a panic (script exhausted)")
		}
	}()
	_, _ = fake.Chat(context.Background(), Request{})
}

// TestFake_recordedRequestsIsSafeUnderConcurrentChat runs concurrent Chat
// calls alongside concurrent RecordedRequests reads (go test -race is
// meant to catch a data race here — the Requests field itself is not safe
// to read this way, which is exactly why RecordedRequests exists).
func TestFake_recordedRequestsIsSafeUnderConcurrentChat(t *testing.T) {
	const n = 50
	results := make([]FakeResult, n)
	for i := range results {
		results[i] = FakeResult{Response: Response{Text: "ok"}}
	}
	fake := NewFake(results...)

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = fake.Chat(context.Background(), Request{
				System: "sys",
				Tools:  []Tool{{Name: "t", InputSchema: json.RawMessage(`{"type":"object"}`)}},
			})
			_ = fake.RecordedRequests()
		}()
	}
	wg.Wait()

	got := fake.RecordedRequests()
	if len(got) != n {
		t.Fatalf("RecordedRequests() len = %d, want %d", len(got), n)
	}
	for _, req := range got {
		if req.System != "sys" || len(req.Tools) != 1 || req.Tools[0].Name != "t" {
			t.Fatalf("RecordedRequests() entry = %+v, want the scripted request", req)
		}
	}

	// Mutating a copy must never reach Fake's own recorded state.
	got[0].Tools[0].Name = "mutated"
	fresh := fake.RecordedRequests()
	if fresh[0].Tools[0].Name != "t" {
		t.Fatalf("RecordedRequests()[0].Tools[0].Name = %q after mutating a prior copy, want unaffected %q", fresh[0].Tools[0].Name, "t")
	}
}
