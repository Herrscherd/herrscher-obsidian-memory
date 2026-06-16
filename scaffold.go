package obsidian

import (
	"context"
	"os"

	"github.com/Herrscherd/herrscher-contracts"
)

// InitSpec describes a project to scaffold. Org is optional; when empty the
// project lives flat under "projets/<Project>".
type InitSpec struct {
	Org     string
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
		return errEmptyProject
	}
	base := s.base()

	if s.Org != "" {
		orgKey := s.Org + "/index"
		if err := m.ensure(ctx, contracts.Node{Key: orgKey, Kind: contracts.KindOrganization,
			Title: s.Org, Links: []contracts.Link{{To: base + "/index", Rel: "contains"}}}); err != nil {
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
	if err := m.ensure(ctx, contracts.Node{Key: projKey, Kind: contracts.KindProject,
		Title: s.Project, Links: projLinks}); err != nil {
		return err
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

// ensure Records the node only if its file does not already exist.
func (m *ObsidianMemory) ensure(ctx context.Context, n contracts.Node) error {
	if _, err := os.Stat(keyToPath(m.root, n.Key)); err == nil {
		return nil // exists — never overwrite
	}
	return m.Record(ctx, n)
}
