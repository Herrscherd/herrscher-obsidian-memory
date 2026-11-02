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

func (spec InitSpec) base() string {
	if spec.Org != "" {
		return slug(spec.Org) + "/" + slug(spec.Project)
	}
	return "projets/" + slug(spec.Project)
}

// Init scaffolds the canonical layout. It only writes nodes that do not yet exist
// (idempotent, never overwrites), and links children to their parents.
func (m *ObsidianMemory) Init(ctx context.Context, spec InitSpec) error {
	if spec.Project == "" {
		return ErrNoProject
	}
	base := spec.base()
	domainKey := spec.domainKey()
	projKey := base + "/index"
	if err := m.ensureOrg(ctx, spec, base); err != nil {
		return err
	}
	if err := m.ensure(ctx, projectNode(spec, base, domainKey)); err != nil {
		return err
	}
	if err := m.ensureDomain(ctx, spec, domainKey, projKey); err != nil {
		return err
	}
	return m.ensureAll(ctx, childNodes(spec, base, projKey))
}

func (spec InitSpec) domainKey() string {
	if spec.Domain == "" {
		return ""
	}
	return "domaines/" + slug(spec.Domain) + "/index"
}

func (m *ObsidianMemory) ensureOrg(ctx context.Context, spec InitSpec, base string) error {
	if spec.Org == "" {
		return nil
	}
	orgKey := slug(spec.Org) + "/index"
	if err := m.ensure(ctx, contracts.Node{
		Key: orgKey, Kind: contracts.KindOrganization,
		Title: spec.Org,
	}); err != nil {
		return err
	}
	return m.Links(ctx, orgKey, base+"/index", relContains)
}

func projectNode(spec InitSpec, base, domainKey string) contracts.Node {
	node := contracts.Node{
		Key: base + "/index", Kind: contracts.KindProject,
		Title: spec.Project, Links: projectLinks(spec, base, domainKey),
	}
	if spec.Domain != "" {
		node.Meta = map[string]string{"domain": spec.Domain}
	}
	return node
}

func projectLinks(spec InitSpec, base, domainKey string) []contracts.Link {
	links := make([]contracts.Link, 0, 4+len(spec.Repos)+len(spec.Servers))
	if spec.Org != "" {
		links = append(links, contracts.Link{To: slug(spec.Org) + "/index", Rel: relBelongsTo})
	}
	links = append(links,
		contracts.Link{To: base + "/architecture", Rel: relContains},
		contracts.Link{To: base + "/production", Rel: relContains})
	for _, repo := range spec.Repos {
		links = append(links, contracts.Link{To: base + "/repos/" + slug(repo), Rel: relContains})
	}
	for _, server := range spec.Servers {
		links = append(links, contracts.Link{To: base + "/servers/" + slug(server), Rel: relContains})
	}
	if domainKey != "" {
		links = append(links, contracts.Link{To: domainKey, Rel: "in-domain"})
	}
	return links
}

func (m *ObsidianMemory) ensureDomain(ctx context.Context, spec InitSpec, domainKey, projKey string) error {
	if spec.Domain == "" {
		return nil
	}
	if err := m.ensure(ctx, contracts.Node{
		Key: domainKey, Kind: contracts.KindDomain,
		Title: spec.Domain,
	}); err != nil {
		return err
	}
	return m.Links(ctx, domainKey, projKey, relContains)
}

func childNodes(spec InitSpec, base, projKey string) []contracts.Node {
	nodes := []contracts.Node{
		{
			Key: base + "/architecture", Kind: contracts.KindArchitecture,
			Title: spec.Project + ": architecture", Links: belongsTo(projKey),
		},
		{
			Key: base + "/production", Kind: contracts.KindProduction,
			Title: spec.Project + ": production", Links: belongsTo(projKey),
		},
	}
	for _, repo := range spec.Repos {
		nodes = append(nodes, contracts.Node{
			Key: base + "/repos/" + slug(repo), Kind: contracts.KindRepo,
			Title: repo, Links: belongsTo(projKey),
		})
	}
	for _, server := range spec.Servers {
		nodes = append(nodes, contracts.Node{
			Key: base + "/servers/" + slug(server), Kind: contracts.KindServer,
			Title: server, Links: belongsTo(projKey),
		})
	}
	return nodes
}

func belongsTo(projKey string) []contracts.Link {
	return []contracts.Link{{To: projKey, Rel: relBelongsTo}}
}

func (m *ObsidianMemory) ensureAll(ctx context.Context, nodes []contracts.Node) error {
	for _, node := range nodes {
		if err := m.ensure(ctx, node); err != nil {
			return err
		}
	}
	return nil
}

func (m *ObsidianMemory) ensure(ctx context.Context, node contracts.Node) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := m.flock(ctx)
	if err != nil {
		return err
	}
	err = m.ensureUnlocked(node)
	return errors.Join(err, release())
}

func (m *ObsidianMemory) ensureUnlocked(node contracts.Node) error {
	if _, err := m.root.Stat(keyToRel(node.Key)); err == nil {
		return nil
	}
	return m.recordUnlockedNoReload(node)
}
