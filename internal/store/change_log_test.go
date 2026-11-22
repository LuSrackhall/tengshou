package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/store"
)

func appendLogs(t *testing.T, st *store.Store, entries ...*model.ChangeLog) {
	t.Helper()
	ctx := context.Background()
	for _, entry := range entries {
		if err := st.AppendChangeLog(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}
}

func logSummaries(items []model.ChangeLog) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Summary)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestListChangeLogsAfterKeepsAppendOrderWithinOneSecond(t *testing.T) {
	st, _ := openStore(t)
	rd := mustReader(t, st)
	ctx := context.Background()
	// created_at is truncated to seconds, so all three rows share a timestamp.
	// The cursor must still follow insertion order (rowid), not random ids.
	at := time.Date(2026, 11, 22, 16, 0, 0, 0, time.UTC)
	first := &model.ChangeLog{Entity: "field", EntityID: "fld_a", Action: "create", Actor: "op", Summary: "a", CreatedAt: at}
	second := &model.ChangeLog{Entity: "field", EntityID: "fld_b", Action: "create", Actor: "op", Summary: "b", CreatedAt: at}
	third := &model.ChangeLog{Entity: "contract", EntityID: "ctr_c", Action: "create", Actor: "tengshou-mcp", Summary: "c", CreatedAt: at}
	appendLogs(t, st, first, second, third)

	items, err := rd.ListChangeLogsAfter(ctx, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := logSummaries(items); !equalStrings(got, []string{"a", "b", "c"}) {
		t.Fatalf("tail order = %v", got)
	}

	items, err = rd.ListChangeLogsAfter(ctx, first.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := logSummaries(items); !equalStrings(got, []string{"b", "c"}) {
		t.Fatalf("after first = %v", got)
	}

	items, err = rd.ListChangeLogsAfter(ctx, third.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("after newest = %v", logSummaries(items))
	}

	// An unknown cursor (database restored from a backup) falls back to the
	// most recent entries so the client can catch up.
	items, err = rd.ListChangeLogsAfter(ctx, "chg_missing", 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := logSummaries(items); !equalStrings(got, []string{"b", "c"}) {
		t.Fatalf("unknown cursor tail = %v", got)
	}
}

func TestListChangeLogsActorAndSinceFilters(t *testing.T) {
	st, _ := openStore(t)
	rd := mustReader(t, st)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	appendLogs(t, st,
		&model.ChangeLog{Entity: "party", EntityID: "p1", Action: "create", Actor: "tester", Summary: "human", CreatedAt: now.Add(-48 * time.Hour)},
		&model.ChangeLog{Entity: "party", EntityID: "p2", Action: "create", Actor: "tengshou-mcp", Summary: "agent", CreatedAt: now.Add(-time.Hour)},
		&model.ChangeLog{Entity: "party", EntityID: "p3", Action: "create", Actor: "tengshou-mcp", Summary: "agent-recent", CreatedAt: now},
	)

	agents, err := rd.ListChangeLogs(ctx, store.ChangeFilter{
		Actor: "tengshou-mcp", Since: now.Add(-24 * time.Hour), Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := logSummaries(agents.Items); !equalStrings(got, []string{"agent-recent", "agent"}) {
		t.Fatalf("agent filter = %v", got)
	}
	if agents.Total != 2 {
		t.Fatalf("agent total = %d", agents.Total)
	}

	humans, err := rd.ListChangeLogs(ctx, store.ChangeFilter{ActorNot: "tengshou-mcp", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if got := logSummaries(humans.Items); !equalStrings(got, []string{"human"}) {
		t.Fatalf("human filter = %v", got)
	}
}
