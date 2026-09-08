package ai

import (
	"context"
	"errors"
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
