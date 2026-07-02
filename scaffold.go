package obsidian

import (
	"context"
	"fmt"

	"github.com/Herrscherd/herrscher-contracts"
)

// InitSpec describes a project to scaffold. Org is optional; when empty the
// project lives flat under "projets/<Project>". Domain is optional; when set it
// attaches the project to a transverse domain root under "domaines/<Domain>".
type InitSpec struct {
	Org     string
	Domain  string
	Project string
	Repos   []string
	Servers []string
}

// base returns the vault path prefix for the project ("<org>/<project>" or
// "projets/<project>").
func (s InitSpec) base() string {
	if s.Org != "" {
		return s.Org + "/" + s.Project
	}
	return "projets/" + s.Project
}

// Init scaffolds the canonical layout. It only writes nodes that do not yet exist
// (idempotent, never overwrites), and links children to their parents.
func (m *ObsidianMemory) Init(ctx context.Context, s InitSpec) error {
	if s.Project == "" {
		return fmt.Errorf("obsidian: Init needs a Project name")
	}
	base := s.base()

	// Domain (optional, transverse): attach the project to a "domaines/<slug>" root.
	var domainKey string
	if s.Domain != "" {
		domainKey = "domaines/" + s.Domain + "/index"
	}

	if s.Org != "" {
		orgKey := s.Org + "/index"
		if err := m.ensure(ctx, contracts.Node{Key: orgKey, Kind: contracts.KindOrganization,
			Title: s.Org}); err != nil {
			return err
		}
		// Append via Links (idempotent) rather than the node literal so a second
		// project under the same org is added instead of dropped (ensure skips an
		// existing org node, so its literal links never grow).
		if err := m.Links(ctx, orgKey, base+"/index", "contains"); err != nil {
			return err
		}
	}

	projKey := base + "/index"
	var projLinks []contracts.Link
	if s.Org != "" {
		projLinks = append(projLinks, contracts.Link{To: s.Org + "/index", Rel: "belongs-to"})
	}
	// The project node links down to every child so Recall(project) surfaces the
	// whole spine; children also link back up (belongs-to) below.
	projLinks = append(projLinks,
		contracts.Link{To: base + "/architecture", Rel: "contains"},
		contracts.Link{To: base + "/production", Rel: "contains"})
	for _, r := range s.Repos {
		projLinks = append(projLinks, contracts.Link{To: base + "/repos/" + r, Rel: "contains"})
	}
	for _, sv := range s.Servers {
		projLinks = append(projLinks, contracts.Link{To: base + "/servers/" + sv, Rel: "contains"})
	}
	if domainKey != "" {
		projLinks = append(projLinks, contracts.Link{To: domainKey, Rel: "in-domain"})
	}
	projNode := contracts.Node{Key: projKey, Kind: contracts.KindProject,
		Title: s.Project, Links: projLinks}
	if s.Domain != "" {
		projNode.Meta = map[string]string{"domain": s.Domain}
	}
	if err := m.ensure(ctx, projNode); err != nil {
		return err
	}
	if domainKey != "" {
		if err := m.ensure(ctx, contracts.Node{Key: domainKey, Kind: contracts.KindDomain,
			Title: s.Domain}); err != nil {
			return err
		}
		// Same reason as the org contains-link above: append via Links so a domain
		// accumulates every project attached to it, not just the first.
		if err := m.Links(ctx, domainKey, projKey, "contains"); err != nil {
			return err
		}
	}
	if err := m.ensure(ctx, contracts.Node{Key: base + "/architecture", Kind: contracts.KindArchitecture,
		Title: s.Project + " — architecture", Links: []contracts.Link{{To: projKey, Rel: "belongs-to"}}}); err != nil {
		return err
	}
	if err := m.ensure(ctx, contracts.Node{Key: base + "/production", Kind: contracts.KindProduction,
		Title: s.Project + " — production", Links: []contracts.Link{{To: projKey, Rel: "belongs-to"}}}); err != nil {
		return err
	}

	for _, r := range s.Repos {
		if err := m.ensure(ctx, contracts.Node{Key: base + "/repos/" + r, Kind: contracts.KindRepo,
			Title: r, Links: []contracts.Link{{To: projKey, Rel: "belongs-to"}}}); err != nil {
			return err
		}
	}
	for _, sv := range s.Servers {
		if err := m.ensure(ctx, contracts.Node{Key: base + "/servers/" + sv, Kind: contracts.KindServer,
			Title: sv, Links: []contracts.Link{{To: projKey, Rel: "belongs-to"}}}); err != nil {
			return err
		}
	}
	return nil
}

// ensure Records the node only if its file does not already exist. The existence
// check and the write are atomic under the same lock Record uses.
func (m *ObsidianMemory) ensure(ctx context.Context, n contracts.Node) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	defer m.flock(ctx)()
	if _, err := m.root.Stat(keyToRel(n.Key)); err == nil {
		return nil // exists — never overwrite
	}
	return m.recordUnlocked(n)
}
