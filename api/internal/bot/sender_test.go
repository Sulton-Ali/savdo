package bot_test

import (
	"context"
	"testing"

	"github.com/Sulton-Ali/savdo/api/internal/ai"
)

// TestHandleUpdate_nilSenderNeverPanics pins item 10's own guard:
// NewService with a nil Sender (cmd/api's own wiring when
// TELEGRAM_BOT_TOKEN is unset — cmd/api/main.go's own doc comment) must
// never let a reply attempt panic on a nil interface; bot.nilSender
// (sender.go) turns every send into a logged error instead. /start never
// calls the LLM, so a nil aiClient (via ai.NewFake with zero scripted
// results, which panics if Chat is ever actually called) proves this
// scenario never reaches Chat either.
func TestHandleUpdate_nilSenderNeverPanics(t *testing.T) {
	env := newTestEnv(t, ai.NewFake())
	env.svc = env.rebuildServiceNilSender()

	// The real assertion is "this does not panic" — HandleUpdate's own
	// error paths (persist.go's own log-and-continue convention) handle
	// nilSender's returned error the same way any other send failure is
	// handled today.
	env.svc.HandleUpdate(context.Background(), textUpdate(1, 100, "alice", "uz", "/start"))
}
