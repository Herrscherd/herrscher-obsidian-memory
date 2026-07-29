package obsidian_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	obsidian "github.com/Herrscherd/herrscher-obsidian-memory"
)

func node(key, body string) contracts.Node {
	return contracts.Node{Key: key, Kind: contracts.KindDecision, Title: "T", Body: body}
}

func TestRecordRejectsOverBudget(t *testing.T) {
	m, err := obsidian.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.SetNodeBudget(50)

	// 100 accented runes = 200 bytes: proves the check counts runes, not bytes.
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
	m, err := obsidian.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.SetNodeBudget(50)
	if err := m.Record(context.Background(), node("projects/x/fact", "short body")); err != nil {
		t.Fatalf("under budget should record, got %v", err)
	}
}

func TestZeroBudgetDisablesCheck(t *testing.T) {
	m, err := obsidian.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	// default budget is 0 (New does not set one) → no enforcement.
	if err := m.Record(context.Background(), node("projects/x/fact", strings.Repeat("x", 10_000))); err != nil {
		t.Fatalf("zero budget must not enforce, got %v", err)
	}
}
