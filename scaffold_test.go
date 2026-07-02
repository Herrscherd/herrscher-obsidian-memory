package obsidian

import (
	"context"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestInitScaffoldsHierarchy(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	spec := InitSpec{
		Org:     "herrscher",
		Project: "herrscher",
		Repos:   []string{"contracts", "gateway"},
		Servers: []string{"vps-1"},
	}
	if err := m.Init(ctx, spec); err != nil {
		t.Fatalf("Init: %v", err)
	}

	proj, err := m.load("herrscher/herrscher/index")
	if err != nil {
		t.Fatalf("project node missing: %v", err)
	}
	if proj.Kind != contracts.KindProject {
		t.Fatalf("project kind wrong: %s", proj.Kind)
	}
	if _, err := m.load("herrscher/herrscher/repos/contracts"); err != nil {
		t.Fatalf("repo node missing: %v", err)
	}
	if _, err := m.load("herrscher/herrscher/servers/vps-1"); err != nil {
		t.Fatalf("server node missing: %v", err)
	}
	if _, err := m.load("herrscher/index"); err != nil {
		t.Fatalf("org node missing: %v", err)
	}
	if _, err := m.load("herrscher/herrscher/architecture"); err != nil {
		t.Fatalf("architecture doc missing: %v", err)
	}
}

func TestInitProjectReachesChildrenViaRecall(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	spec := InitSpec{Org: "org", Project: "proj", Repos: []string{"r1"}, Servers: []string{"s1"}}
	if err := m.Init(ctx, spec); err != nil {
		t.Fatalf("Init: %v", err)
	}
	sg, err := m.Recall(ctx, "org/proj/index", 1)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	reached := map[string]bool{}
	for _, n := range sg.Nodes {
		reached[n.Key] = true
	}
	for _, want := range []string{"org/proj/architecture", "org/proj/production", "org/proj/repos/r1", "org/proj/servers/s1"} {
		if !reached[want] {
			t.Fatalf("Recall(project) did not reach %q: %v", want, reached)
		}
	}
}

func TestInitIsIdempotentAndNeverOverwrites(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	spec := InitSpec{Project: "solo", Repos: []string{"solo"}}
	if err := m.Init(ctx, spec); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	rel := keyToRel("projets/solo/index")
	if err := m.root.WriteFile(rel, []byte("---\ntype: project\n---\nHUMAN EDIT\n"), 0o644); err != nil {
		t.Fatalf("hand edit: %v", err)
	}
	if err := m.Init(ctx, spec); err != nil {
		t.Fatalf("second Init: %v", err)
	}
	got, _ := m.root.ReadFile(rel)
	if !strings.Contains(string(got), "HUMAN EDIT") {
		t.Fatalf("Init overwrote a human-edited file: %q", string(got))
	}
}

func TestInitWithDomainScaffoldsDomainNodeAndLinks(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	spec := InitSpec{Domain: "dev", Project: "proj"}
	if err := m.Init(ctx, spec); err != nil {
		t.Fatalf("Init: %v", err)
	}

	dom, err := m.load("domaines/dev/index")
	if err != nil {
		t.Fatalf("domain node missing: %v", err)
	}
	if dom.Kind != contracts.KindDomain {
		t.Fatalf("domain kind = %s, want %s", dom.Kind, contracts.KindDomain)
	}
	foundContains := false
	for _, l := range dom.Links {
		if l.To == "projets/proj/index" && l.Rel == "contains" {
			foundContains = true
		}
	}
	if !foundContains {
		t.Fatalf("domain does not contain project: %+v", dom.Links)
	}

	proj, err := m.load("projets/proj/index")
	if err != nil {
		t.Fatalf("project node missing: %v", err)
	}
	if proj.Meta["domain"] != "dev" {
		t.Fatalf("project domain meta = %q, want dev", proj.Meta["domain"])
	}
	foundInDomain := false
	for _, l := range proj.Links {
		if l.To == "domaines/dev/index" && l.Rel == "in-domain" {
			foundInDomain = true
		}
	}
	if !foundInDomain {
		t.Fatalf("project not linked in-domain: %+v", proj.Links)
	}
}

func TestInitWithoutDomainIsUnchanged(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	if err := m.Init(ctx, InitSpec{Project: "solo"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := m.load("domaines/solo/index"); err == nil {
		t.Fatalf("no-domain Init created a domain node")
	}
	proj, err := m.load("projets/solo/index")
	if err != nil {
		t.Fatalf("project missing: %v", err)
	}
	if _, has := proj.Meta["domain"]; has {
		t.Fatalf("no-domain Init stamped a domain meta: %+v", proj.Meta)
	}
}

func TestRecallDomainReachesProject(t *testing.T) {
	m := newTestMem(t)
	ctx := context.Background()
	if err := m.Init(ctx, InitSpec{Domain: "dev", Project: "proj"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	sg, err := m.Recall(ctx, "domaines/dev/index", 1)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	reached := map[string]bool{}
	for _, n := range sg.Nodes {
		reached[n.Key] = true
	}
	if !reached["projets/proj/index"] {
		t.Fatalf("Recall(domain) did not reach project: %v", reached)
	}
}
