package obsidian

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func node(key, body string) contracts.Node {
	return contracts.Node{Key: key, Kind: contracts.KindDecision, Title: "T", Body: body}
}

func TestRecordRejectsOverBudget(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, m)
	m.SetNodeBudget(50)

	err = m.Record(context.Background(), node("projects/x/fact", strings.Repeat("é", 100)))
	var be *contracts.BudgetError
	if !errors.As(err, &be) {
		t.Fatalf("want *BudgetError, got %v", err)
	}
	if be.Runes != 100 || be.Limit != 50 {
		t.Fatalf("unexpected sizes: %+v", be)
	}
}

func TestRecordAllowsUnderBudget(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, m)
	m.SetNodeBudget(50)
	if err := m.Record(context.Background(), node("projects/x/fact", "short body")); err != nil {
		t.Fatalf("under budget should record, got %v", err)
	}
}

func TestZeroBudgetDisablesCheck(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, m)
	if err := m.Record(context.Background(), node("projects/x/fact", strings.Repeat("x", 10_000))); err != nil {
		t.Fatalf("zero budget must not enforce, got %v", err)
	}
}

func TestRecordTranscriptBypassesBudget(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, m)
	m.SetNodeBudget(10)
	ctx := context.Background()
	big := strings.Repeat("x", 500)

	err = m.Record(ctx, contracts.Node{Key: "projects/p/fact", Kind: contracts.KindProject, Body: big})
	var budgetErr *contracts.BudgetError
	if !errors.As(err, &budgetErr) {
		t.Fatalf("distilled over-budget Record: got %v, want *BudgetError", err)
	}

	if err = m.Record(ctx, contracts.Node{Key: "raw/s/1", Kind: contracts.KindTranscript, Body: big}); err != nil {
		t.Fatalf("raw over-budget Record must succeed, got %v", err)
	}
	got, err := m.Search(ctx, contracts.Query{Text: "xxx", IncludeRaw: true})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range got {
		if n.Key == "raw/s/1" {
			found = true
			if utf8.RuneCountInString(n.Body) != 500 {
				t.Fatalf("raw body truncated to %d runes; archival must preserve all 500", utf8.RuneCountInString(n.Body))
			}
		}
	}
	if !found {
		t.Fatal("raw node not found after Record")
	}
}
