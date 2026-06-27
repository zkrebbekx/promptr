package livetest

import (
	"os"
	"testing"

	"github.com/zkrebbekx/promptr/providers/fake"
)

// TestMain wires the generated promptr tests (ticket.promptr_test.go) to a
// deterministic provider so they run in CI without a network or API key. The
// fake replies with one scripted ticket; schema-aligned parsing coerces it into
// the typed Ticket the assertions check. Swap in providers/recorded for captured
// real responses, or a live provider to run the same blocks against a model.
func TestMain(m *testing.M) {
	PromptrProvider = fake.New(
		`{"title": "Server is down", "severity": "CRITICAL", "open": true, "votes": 3}`,
	)
	os.Exit(m.Run())
}
