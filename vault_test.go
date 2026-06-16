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

func TestUnmarshalNoFrontmatter(t *testing.T) {
	got := unmarshalNode("loose/note", []byte("just a body, no fences\n"))
	if got.Key != "loose/note" || got.Body == "" {
		t.Fatalf("body-only note mishandled: %+v", got)
	}
}
