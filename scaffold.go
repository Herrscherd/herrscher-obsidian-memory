package obsidian

import (
	"context"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func slug(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

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

func (s InitSpec) base() string {
	if s.Org != "" {
		return slug(s.Org) + "/" + slug(s.Project)
	}
	return "projets/" + slug(s.Project)
}

// Init scaffolds the canonical layout. It only writes nodes that do not yet exist
// (idempotent, never overwrites), and links children to their parents.
func (m *ObsidianMemory) Init(ctx context.Context, s InitSpec) error {
	if s.Project == "" {
		return ErrNoProject
	}
	base := s.base()

	var domainKey string
	if s.Domain != "" {
		domainKey = "domaines/" + slug(s.Domain) + "/index"
	}

	if s.Org != "" {
		orgKey := slug(s.Org) + "/index"
		if err := m.ensure(ctx, contracts.Node{
			Key: orgKey, Kind: contracts.KindOrganization,
			Title: s.Org,
		}); err != nil {
			return err
		}
		if err := m.Links(ctx, orgKey, base+"/index", "contains"); err != nil {
			return err
		}
	}

	projKey := base + "/index"
	projLinks := make([]contracts.Link, 0, 4+len(s.Repos)+len(s.Servers))
	if s.Org != "" {
		projLinks = append(projLinks, contracts.Link{To: slug(s.Org) + "/index", Rel: "belongs-to"})
	}
	projLinks = append(projLinks,
		contracts.Link{To: base + "/architecture", Rel: "contains"},
		contracts.Link{To: base + "/production", Rel: "contains"})
	for _, r := range s.Repos {
		projLinks = append(projLinks, contracts.Link{To: base + "/repos/" + slug(r), Rel: "contains"})
	}
	for _, sv := range s.Servers {
		projLinks = append(projLinks, contracts.Link{To: base + "/servers/" + slug(sv), Rel: "contains"})
	}
	if domainKey != "" {
		projLinks = append(projLinks, contracts.Link{To: domainKey, Rel: "in-domain"})
	}
	projNode := contracts.Node{
		Key: projKey, Kind: contracts.KindProject,
		Title: s.Project, Links: projLinks,
	}
	if s.Domain != "" {
		projNode.Meta = map[string]string{"domain": s.Domain}
	}
	if err := m.ensure(ctx, projNode); err != nil {
		return err
	}
	if domainKey != "" {
		if err := m.ensure(ctx, contracts.Node{
			Key: domainKey, Kind: contracts.KindDomain,
			Title: s.Domain,
		}); err != nil {
			return err
		}
		if err := m.Links(ctx, domainKey, projKey, "contains"); err != nil {
			return err
		}
	}
	if err := m.ensure(ctx, contracts.Node{
		Key: base + "/architecture", Kind: contracts.KindArchitecture,
		Title: s.Project + ": architecture", Links: []contracts.Link{{To: projKey, Rel: "belongs-to"}},
	}); err != nil {
		return err
	}
	if err := m.ensure(ctx, contracts.Node{
		Key: base + "/production", Kind: contracts.KindProduction,
		Title: s.Project + ": production", Links: []contracts.Link{{To: projKey, Rel: "belongs-to"}},
	}); err != nil {
		return err
	}

	for _, r := range s.Repos {
		if err := m.ensure(ctx, contracts.Node{
			Key: base + "/repos/" + slug(r), Kind: contracts.KindRepo,
			Title: r, Links: []contracts.Link{{To: projKey, Rel: "belongs-to"}},
		}); err != nil {
			return err
		}
	}
	for _, sv := range s.Servers {
		if err := m.ensure(ctx, contracts.Node{
			Key: base + "/servers/" + slug(sv), Kind: contracts.KindServer,
			Title: sv, Links: []contracts.Link{{To: projKey, Rel: "belongs-to"}},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m *ObsidianMemory) ensure(ctx context.Context, n contracts.Node) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := m.flock(ctx)
	if err != nil {
		return err
	}
	defer release()
	if _, err := m.root.Stat(keyToRel(n.Key)); err == nil {
		return nil
	}
	return m.recordUnlockedNoReload(n)
}
