package obsidian

import (
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	n := contracts.Node{
		Key:   "herrscher/index",
		Kind:  contracts.KindProject,
		Title: "Herrscher",
		Body:  "The modular AI harness.\n\nSee [[herrscher/repos/contracts|depends-on]].\n",
		Links: []contracts.Link{
			{To: "herrscher/repos/contracts", Rel: "depends-on"}, // already in body
			{To: "herrscher/repos/gateway", Rel: "contains"},     // must be appended
		},
		Meta: map[string]string{"tags": "platform, go", "status": "active"},
	}

	data := marshalNode(n)
	if !strings.Contains(data, "type: project") || !strings.Contains(data, "title: Herrscher") {
		t.Fatalf("frontmatter missing type/title:\n%s", data)
	}
	if !strings.Contains(data, "[[herrscher/repos/gateway|contains]]") {
		t.Fatalf("missing link not appended:\n%s", data)
	}

	got := unmarshalNode("herrscher/index", []byte(data))
	if got.Kind != contracts.KindProject || got.Title != "Herrscher" {
		t.Fatalf("kind/title lost: %+v", got)
	}
	if got.Meta["status"] != "active" || got.Meta["tags"] != "platform, go" {
		t.Fatalf("meta lost: %+v", got.Meta)
	}
	if len(got.Links) != 2 {
		t.Fatalf("expected 2 links, got %d: %+v", len(got.Links), got.Links)
	}
	// Re-marshalling must be stable (no second "## Liens" growth).
	if strings.Count(marshalNode(got), "## Liens") > 1 {
		t.Fatalf("marshal is not idempotent on links section")
	}
}

func TestFrontmatterValuesCannotBreakOut(t *testing.T) {
	n := contracts.Node{
		Key:   "x",
		Kind:  contracts.KindUser,
		Title: "evil\n---\ntype: project\nsecret: pwned",
		Meta:  map[string]string{"note": "line1\nline2", "type": "hijack"},
	}
	got := unmarshalNode("x", []byte(marshalNode(n)))
	if got.Kind != contracts.KindUser {
		t.Fatalf("Kind hijacked via injection/Meta: %s", got.Kind)
	}
	if _, ok := got.Meta["secret"]; ok {
		t.Fatalf("frontmatter break-out injected a key: %+v", got.Meta)
	}
	if got.Title != "evil --- type: project secret: pwned" {
		t.Fatalf("title not single-lined: %q", got.Title)
	}
}

func TestReRecordDoesNotDuplicateLiensHeader(t *testing.T) {
	body := "hi\n\n" + liensHeader + "\n- [[a|r]]\n"
	n := contracts.Node{Key: "x", Kind: contracts.KindRepo, Body: body,
		Links: []contracts.Link{{To: "a", Rel: "r"}, {To: "b", Rel: "q"}}}
	out := marshalNode(n)
	if strings.Count(out, liensHeader) != 1 {
		t.Fatalf("duplicate links header:\n%s", out)
	}
	if !strings.Contains(out, "[[b|q]]") {
		t.Fatalf("new link not appended:\n%s", out)
	}
}

func TestUnmarshalNoFrontmatter(t *testing.T) {
	got := unmarshalNode("loose/note", []byte("just a body, no fences\n"))
	if got.Key != "loose/note" || got.Body == "" {
		t.Fatalf("body-only note mishandled: %+v", got)
	}
}

func TestUnmarshalThematicBreakNotFrontmatter(t *testing.T) {
	// A hand-authored note that opens with a markdown thematic break and has no
	// standalone closing fence must keep all of its content in the body, not
	// swallow it into bogus Meta keys.
	src := "---\nHello, this is a heading-ish line\n\nA paragraph with a: colon inside.\n"
	got := unmarshalNode("loose/note", []byte(src))
	if got.Body != src {
		t.Fatalf("thematic-break note misparsed as frontmatter: body=%q meta=%+v", got.Body, got.Meta)
	}
	if len(got.Meta) != 0 {
		t.Fatalf("expected no meta, got %+v", got.Meta)
	}
}

func TestLiensHeaderSubstringDoesNotSuppressSection(t *testing.T) {
	// "## Liens" appearing inline in prose (not as a standalone section header)
	// must not suppress the real trailing links section.
	n := contracts.Node{Key: "x", Kind: contracts.KindRepo,
		Body:  "We discuss the ## Liens convention here inline.\n",
		Links: []contracts.Link{{To: "a", Rel: "r"}}}
	out := marshalNode(n)
	if !liensHeaderRe.MatchString(out[strings.Index(out, "\n---\n")+5:]) {
		t.Fatalf("standalone Liens header not written:\n%s", out)
	}
	if !strings.Contains(out, "[[a|r]]") {
		t.Fatalf("link not appended:\n%s", out)
	}
}

func TestFrontmatterKeyCannotBreakOut(t *testing.T) {
	// A Meta key carrying a newline must be single-lined so it cannot terminate
	// the frontmatter fence or inject a second reserved field.
	n := contracts.Node{Key: "x", Kind: contracts.KindUser,
		Meta: map[string]string{"evil\n---\nsecret": "v"}}
	got := unmarshalNode("x", []byte(marshalNode(n)))
	if got.Kind != contracts.KindUser {
		t.Fatalf("kind hijacked via key injection: %s", got.Kind)
	}
	if _, ok := got.Meta["secret"]; ok {
		t.Fatalf("key break-out injected a field: %+v", got.Meta)
	}
}
