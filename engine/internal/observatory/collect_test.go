package observatory

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/semyonfox/tokentelemetry/engine/internal/ingest"
	"github.com/semyonfox/tokentelemetry/engine/internal/model"
	"github.com/semyonfox/tokentelemetry/engine/internal/pricing"
)

// Synthetic turns only; no personal logs are read.
func TestBuildCollectionAggregatesBySourceAndModel(t *testing.T) {
	tbl, err := pricing.Load()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.Local)
	c := Collection{ObservedAt: now, From: "2026-09-26", To: "2026-10-25",
		Coverage: []Coverage{{Agent: "claude"}, {Agent: "codex"}, {Agent: "gemini"}}}
	ctx := func(n int64) model.Usage { return model.Usage{Input: 100, Output: 10, ContextTokens: n} }
	turns := []model.Turn{
		{Key: "a", SessionID: "secret-session", Project: `C:\private\project`, Agent: "claude", Model: "mystery-model-x", Timestamp: now.Add(-2 * time.Hour), Usage: ctx(1000)},
		{Key: "b", SessionID: "secret-session", Agent: "claude", Model: "mystery-model-x", Timestamp: now.Add(-1 * time.Hour), Usage: ctx(2500)},
		{Key: "c", Agent: "codex", Model: "mystery-model-y", Provider: "openai", Timestamp: now.Add(-3 * time.Hour), Usage: model.Usage{Input: 40, Output: 5, CacheRead: 7}},
		// Excluded overlap and out-of-window turns must not be counted.
		{Key: "d", Agent: "codex", Model: "mystery-model-y", Provider: "openai", Timestamp: now.Add(-time.Hour), Usage: model.Usage{Input: 999}, ExcludedReason: "overlap"},
		{Key: "e", Agent: "claude", Model: "mystery-model-x", Timestamp: now.AddDate(0, 0, -60), Usage: model.Usage{Input: 999}},
	}
	windows := []model.PlanWindow{
		{Agent: "codex", Window: "5h", UsedPercent: 95, ObservedAt: now.Add(-5 * time.Minute), ResetsAt: now.Add(time.Hour)},
		{Agent: "claude", Window: "7d", UsedPercent: 99, ObservedAt: now.Add(-2 * time.Hour), ResetsAt: now.Add(24 * time.Hour)},
	}
	got := buildCollection(c, &ingest.Result{Turns: turns}, tbl, windows, now)

	if len(got.Rows) != 2 {
		t.Fatalf("rows = %d, want 2: %+v", len(got.Rows), got.Rows)
	}
	claude, codex := got.Rows[0], got.Rows[1]
	if claude.Agent != "claude" || claude.Usage.Input != 200 || claude.Usage.Output != 20 || claude.Records != 2 {
		t.Fatalf("claude row = %+v", claude)
	}
	if claude.Unpriced != 2 || claude.ListValue != 0 {
		t.Fatalf("unknown model must stay unpriced: %+v", claude)
	}
	if claude.Provider != "Not recorded" || claude.LastContext == nil || *claude.LastContext != 2500 {
		t.Fatalf("provider/context = %q %v", claude.Provider, claude.LastContext)
	}
	if codex.Usage.Input != 40 || codex.Usage.CacheRead != 7 || codex.Provider != "openai" || codex.LastContext != nil {
		t.Fatalf("codex row = %+v", codex)
	}
	cov := map[string]Coverage{}
	for _, x := range got.Coverage {
		cov[x.Agent] = x
	}
	if cov["claude"].Records != 2 || cov["codex"].Records != 1 || cov["gemini"].Records != 0 {
		t.Fatalf("coverage = %+v", got.Coverage)
	}
	if !got.Windows[0].Fresh || got.Windows[1].Fresh {
		t.Fatalf("freshness = %+v", got.Windows)
	}

	// Only aggregates may cross the collector boundary.
	b, _ := json.Marshal(got)
	for _, leak := range []string{"secret-session", "private", "session_id", "project"} {
		if strings.Contains(string(b), leak) {
			t.Fatalf("collection leaks %q", leak)
		}
	}

	f := Fleet{Nodes: []Collection{got, {Machine: "Mac", Error: "disabled"}}}
	notes := fleetSuggestions(f, defaultConfig(), now)
	if len(notes) != 1 || !strings.Contains(notes[0], "codex 5h") {
		t.Fatalf("only the fresh window over threshold should warn: %v", notes)
	}
	cfg := defaultConfig()
	cfg.QuotaWarning = 96
	if notes = fleetSuggestions(f, cfg, now); strings.Contains(notes[0], "codex") {
		t.Fatalf("threshold ignored: %v", notes)
	}
}

func TestQuotaThresholdValidation(t *testing.T) {
	c := defaultConfig()
	c.QuotaWarning = 0
	if c.validate() == nil {
		t.Fatal("zero quota threshold accepted")
	}
}
