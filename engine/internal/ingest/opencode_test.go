package ingest

import (
	"path/filepath"
	"testing"

	"github.com/semyonfox/tokentelemetry/engine/internal/model"
)

func TestOpenCodeModernRowsMaskFrozenLegacyAndKeepOldHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.db")
	db := openKiloFixture(t, path, true, true)
	insertKiloCurrent(t, db, "new", "ses_child", "assistant", 1788256800000, `{"model":{"id":"m","providerID":"openai"},"tokens":{"input":100,"output":20,"cache":{"read":700,"write":50}}}`)
	insertKiloLegacy(t, db, "new", "ses_child", 1788256800000, `{"role":"assistant","modelID":"old","tokens":{"input":999,"output":999}}`)
	insertKiloLegacy(t, db, "old", "ses_main", 1788256800000, `{"role":"assistant","modelID":"old","tokens":{"input":10,"output":5}}`)
	turns := scan(t, &OpenCode{dbPath: path})
	if len(turns) != 2 {
		t.Fatal(turns)
	}
	var input, output int64
	var children int
	for _, turn := range turns {
		input += turn.Usage.Input
		output += turn.Usage.Output
		if turn.Subagent {
			children++
		}
		if string(turn.Agent) != "opencode" {
			t.Fatal(turn)
		}
	}
	if input != 110 || output != 25 || children != 1 {
		t.Fatalf("modern/legacy reconciliation failed: %d %d %d", input, output, children)
	}
	// The intermediate v2 schema has separate metadata and no legacy records.
	if _, err := db.Exec(`ALTER TABLE session RENAME TO session_v2; DROP TABLE message`); err != nil {
		t.Fatal(err)
	}
	turns = scan(t, &OpenCode{dbPath: path})
	if len(turns) != 1 || turns[0].Project != "/work/child" {
		t.Fatal(turns)
	}
}

// OpenCode reports reasoning beside output, not inside it: every audited
// message satisfies total = input + output + reasoning + cache.read +
// cache.write. Treating it as a subset of output left 2.7M tokens uncounted.
func TestOpenCodeCountsReasoningAsOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.db")
	db := openKiloFixture(t, path, true, true)
	// 100 + 20 + 30 + 700 + 50 = 900
	insertKiloCurrent(t, db, "modern", "ses_main", "assistant", 1788256800000, `{"model":{"id":"m","providerID":"openai"},"tokens":{"total":900,"input":100,"output":20,"reasoning":30,"cache":{"read":700,"write":50}}}`)
	// 10 + 5 + 8 + 40 = 63, and output below reasoning cannot hold it
	insertKiloLegacy(t, db, "legacy", "ses_main", 1788256800000, `{"role":"assistant","modelID":"old","tokens":{"total":63,"input":10,"output":5,"reasoning":8,"cache":{"read":40,"write":0}}}`)
	turns := scan(t, &OpenCode{dbPath: path})
	if len(turns) != 2 {
		t.Fatal(turns)
	}
	var sum model.Usage
	for _, turn := range turns {
		sum.Add(turn.Usage)
	}
	want := model.Usage{Input: 110, Output: 63, Reasoning: 38, CacheRead: 740, CacheWrite: 50}
	if sum != want {
		t.Fatalf("usage = %+v, want %+v", sum, want)
	}
	// reasoning stays an informational subset, so the total is the payload's
	if sum.Total() != 900+63 {
		t.Fatalf("total = %d, want %d", sum.Total(), 900+63)
	}
}
