package obsidian

import (
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

// A key is written into other notes as `[[key]]` and read back by a regexp, so a
// key the syntax cannot carry is not a cosmetic problem: it comes back as a
// different key, or as a truncated one, on the next read and never at write time.
func TestKeysAWikilinkCannotCarryAreRefused(t *testing.T) {
	for _, key := range []string{"a|b", "a]b", "a[b", "a\nb", "a\rb"} {
		if err := validKey(key); err == nil {
			t.Errorf("validKey(%q) = nil, want a refusal", key)
		}
	}
	// The rule must not widen past what it is for: these are ordinary keys.
	for _, key := range []string{"plain", "project/herrscher", "a-b_c.d", "élan"} {
		if err := validKey(key); err != nil {
			t.Errorf("validKey(%q) = %v, want it accepted", key, err)
		}
	}
}

func TestRecordRefusesAnUnusableLinkTarget(t *testing.T) {
	m := newTestMem(t)
	err := m.Record(t.Context(), contracts.Node{
		Key:   "k",
		Body:  "hello",
		Links: []contracts.Link{{To: "a|b"}},
	})
	if err == nil {
		t.Fatal("Record accepted a link target that cannot round-trip")
	}
	if !strings.Contains(err.Error(), "a|b") {
		t.Errorf("error = %v, want it to name the offending target", err)
	}
}

func TestRecordRefusesAnUnusableRelation(t *testing.T) {
	m := newTestMem(t)
	err := m.Record(t.Context(), contracts.Node{
		Key:   "k",
		Body:  "hello",
		Links: []contracts.Link{{To: "other", Rel: "sees]] and"}},
	})
	if err == nil {
		t.Fatal("Record accepted a relation that cannot round-trip")
	}
}

// The guarantee the refusal buys: everything that is accepted comes back
// unchanged.
func TestAcceptedLinksRoundTrip(t *testing.T) {
	for _, l := range []contracts.Link{
		{To: "plain"},
		{To: "project/herrscher", Rel: "concerne"},
		{To: "a-b_c.d"},
	} {
		n := contracts.Node{Key: "k", Body: "hello", Links: []contracts.Link{l}}
		got := unmarshalNode("k", []byte(marshalNode(n)))
		if len(got.Links) != 1 || got.Links[0] != l {
			t.Errorf("link %+v round-tripped to %+v", l, got.Links)
		}
	}
}
