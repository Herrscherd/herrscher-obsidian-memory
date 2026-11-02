package obsidian

import (
	"context"
	"errors"
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
	domainKey := s.domainKey()
	projKey := base + "/index"
	if err := m.ensureOrg(ctx, s, base); err != nil {
		return err
	}
	if err := m.ensure(ctx, projectNode(s, base, domainKey)); err != nil {
		return err
	}
	if err := m.ensureDomain(ctx, s, domainKey, projKey); err != nil {
		return err
	}
	return m.ensureAll(ctx, childNodes(s, base, projKey))
}

func (s InitSpec) domainKey() string {
	if s.Domain == "" {
		return ""
	}
	return "domaines/" + slug(s.Domain) + "/index"
}

func (m *ObsidianMemory) ensureOrg(ctx context.Context, s InitSpec, base string) error {
	if s.Org == "" {
		return nil
	}
	orgKey := slug(s.Org) + "/index"
	if err := m.ensure(ctx, contracts.Node{
		Key: orgKey, Kind: contracts.KindOrganization,
		Title: s.Org,
	}); err != nil {
		return err
	}
	return m.Links(ctx, orgKey, base+"/index", relContains)
}

func projectNode(s InitSpec, base, domainKey string) contracts.Node {
	node := contracts.Node{
		Key: base + "/index", Kind: contracts.KindProject,
		Title: s.Project, Links: projectLinks(s, base, domainKey),
	}
	if s.Domain != "" {
		node.Meta = map[string]string{"domain": s.Domain}
	}
	return node
}

func projectLinks(s InitSpec, base, domainKey string) []contracts.Link {
	links := make([]contracts.Link, 0, 4+len(s.Repos)+len(s.Servers))
	if s.Org != "" {
		links = append(links, contracts.Link{To: slug(s.Org) + "/index", Rel: relBelongsTo})
	}
	links = append(links,
		contracts.Link{To: base + "/architecture", Rel: relContains},
		contracts.Link{To: base + "/production", Rel: relContains})
	for _, r := range s.Repos {
		links = append(links, contracts.Link{To: base + "/repos/" + slug(r), Rel: relContains})
	}
	for _, sv := range s.Servers {
		links = append(links, contracts.Link{To: base + "/servers/" + slug(sv), Rel: relContains})
	}
	if domainKey != "" {
		links = append(links, contracts.Link{To: domainKey, Rel: "in-domain"})
	}
	return links
}

func (m *ObsidianMemory) ensureDomain(ctx context.Context, s InitSpec, domainKey, projKey string) error {
	if s.Domain == "" {
		return nil
	}
	if err := m.ensure(ctx, contracts.Node{
		Key: domainKey, Kind: contracts.KindDomain,
		Title: s.Domain,
	}); err != nil {
		return err
	}
	return m.Links(ctx, domainKey, projKey, relContains)
}

func childNodes(s InitSpec, base, projKey string) []contracts.Node {
	nodes := []contracts.Node{
		{
			Key: base + "/architecture", Kind: contracts.KindArchitecture,
			Title: s.Project + ": architecture", Links: belongsTo(projKey),
		},
		{
			Key: base + "/production", Kind: contracts.KindProduction,
			Title: s.Project + ": production", Links: belongsTo(projKey),
		},
	}
	for _, r := range s.Repos {
		nodes = append(nodes, contracts.Node{
			Key: base + "/repos/" + slug(r), Kind: contracts.KindRepo,
			Title: r, Links: belongsTo(projKey),
		})
	}
	for _, sv := range s.Servers {
		nodes = append(nodes, contracts.Node{
			Key: base + "/servers/" + slug(sv), Kind: contracts.KindServer,
			Title: sv, Links: belongsTo(projKey),
		})
	}
	return nodes
}

func belongsTo(projKey string) []contracts.Link {
	return []contracts.Link{{To: projKey, Rel: relBelongsTo}}
}

func (m *ObsidianMemory) ensureAll(ctx context.Context, nodes []contracts.Node) error {
	for _, n := range nodes {
		if err := m.ensure(ctx, n); err != nil {
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
	err = m.ensureUnlocked(n)
	return errors.Join(err, release())
}

func (m *ObsidianMemory) ensureUnlocked(n contracts.Node) error {
	if _, err := m.root.Stat(keyToRel(n.Key)); err == nil {
		return nil
	}
	return m.recordUnlockedNoReload(n)
}
