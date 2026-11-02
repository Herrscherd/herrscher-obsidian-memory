// Package obsidian implements the contracts.Memory port over an Obsidian-style
// markdown vault: one node per .md file, frontmatter for Meta, [[wikilinks]] for
// Links. The vault is a git-versioned folder; Obsidian is the human UI over it.
package obsidian

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const lockName = ".herrscher.lock"

const lockPollInterval = 10 * time.Millisecond

const (
	privateDirMode      fs.FileMode = 0o700
	privateFileMode     fs.FileMode = 0o600
	recencyWeight                   = 0.5
	recencyHalfLifeDays             = 30
	hoursPerDay                     = 24
)

// ObsidianMemory implements contracts.Memory over a markdown vault. All file I/O
// goes through an *os.Root so a malicious key or an in-vault symlink can never
// escape the root. The mutex serializes writes within this process; lockFile
// serializes them across processes (the daemon spawns one bridge subprocess per
// session, all sharing the same vault), and every write lands atomically via a
// temp file + rename so a vault never sees a torn document.
type ObsidianMemory struct {
	mu       sync.Mutex
	root     *os.Root
	lockFile *os.File
	now      func() time.Time

	parseCache map[string]cachedNode

	budget int
}

type cachedNode struct {
	mod  time.Time
	size int64
	node contracts.Node
}

// New opens (creating if absent) a vault directory and returns a Memory over it.
func New(root string) (*ObsidianMemory, error) {
	if root == "" {
		return nil, ErrEmptyRoot
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("obsidian: resolve vault root: %w", err)
	}
	if err = os.MkdirAll(root, privateDirMode); err != nil {
		return nil, fmt.Errorf("obsidian: create vault root: %w", err)
	}
	vaultRoot, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("obsidian: open vault root: %w", err)
	}
	vaultLock, err := vaultRoot.OpenFile(lockName, os.O_CREATE|os.O_RDWR, privateFileMode)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("obsidian: open vault lock: %w", err), vaultRoot.Close())
	}
	return &ObsidianMemory{root: vaultRoot, lockFile: vaultLock, now: time.Now, parseCache: make(map[string]cachedNode)}, nil
}

func (m *ObsidianMemory) flock(ctx context.Context) (func() error, error) {
	for {
		err := lockFD(m.lockFile.Fd())
		if err == nil {
			return func() error { return unlockFD(m.lockFile.Fd()) }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("obsidian: vault lock: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("obsidian: vault lock: %w", ctx.Err())
		case <-time.After(lockPollInterval):
		}
	}
}

func cloneNode(node contracts.Node) contracts.Node {
	node.Meta = maps.Clone(node.Meta)
	node.Links = slices.Clone(node.Links)
	return node
}

// SetNodeBudget sets the per-node Body budget in runes; 0 (the default) disables
// enforcement. When positive, Record returns *contracts.BudgetError for any node
// whose Body exceeds it, so the caller must consolidate or replace the body to fit (G1).
func (m *ObsidianMemory) SetNodeBudget(runes int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.budget = runes
}

func (m *ObsidianMemory) loadUnlocked(key string) (contracts.Node, error) {
	if err := validKey(key); err != nil {
		return contracts.Node{}, err
	}
	data, err := m.root.ReadFile(keyToRel(key))
	if err != nil {
		return contracts.Node{}, fmt.Errorf("obsidian: load %query: %w", key, err)
	}
	return unmarshalNode(key, data), nil
}

func (m *ObsidianMemory) recordUnlocked(node contracts.Node) error {
	if m.budget > 0 && node.Kind != contracts.KindTranscript {
		if r := utf8.RuneCountInString(node.Body); r > m.budget {
			return &contracts.BudgetError{Key: node.Key, Runes: r, Limit: m.budget}
		}
	}
	return m.writeNode(node, true)
}

func (m *ObsidianMemory) recordUnlockedNoReload(node contracts.Node) error {
	return m.writeNode(node, false)
}

func (m *ObsidianMemory) writeNode(node contracts.Node, reloadPrior bool) error {
	if err := validateNodeKeys(node); err != nil {
		return err
	}
	node = m.stampedNode(node, reloadPrior)
	rel := keyToRel(node.Key)
	if err := m.ensureParentDir(rel, node.Key); err != nil {
		return err
	}
	return m.commitNode(rel, node)
}

func validateNodeKeys(node contracts.Node) error {
	if err := validKey(node.Key); err != nil {
		return err
	}
	for _, link := range node.Links {
		if err := validKey(link.To); err != nil {
			return fmt.Errorf("obsidian: node %query links target an unusable key: %w", node.Key, err)
		}
		if i := strings.IndexAny(link.Rel, wikilinkMeta); i >= 0 {
			return fmt.Errorf("%w: node %query link relation %query contains %query, which a wikilink cannot carry", ErrInvalidLink, node.Key, link.Rel, link.Rel[i:i+1])
		}
	}
	return nil
}

func (m *ObsidianMemory) stampedNode(node contracts.Node, reloadPrior bool) contracts.Node {
	node.Meta = maps.Clone(node.Meta)
	if node.Meta == nil {
		node.Meta = make(map[string]string)
	}
	if node.Meta["capturedAt"] == "" {
		node.Meta["capturedAt"] = m.capturedAtFor(node.Key, reloadPrior)
	}
	if node.Meta[contracts.MetaLastSeen] == "" {
		node.Meta[contracts.MetaLastSeen] = m.nowStamp()
	}
	return node
}

func (m *ObsidianMemory) capturedAtFor(key string, reloadPrior bool) string {
	stamp := m.nowStamp()
	if reloadPrior {
		if existing, err := m.loadUnlocked(key); err == nil {
			if prior := existing.Meta["capturedAt"]; prior != "" {
				return prior
			}
		}
	}
	return stamp
}

func (m *ObsidianMemory) nowStamp() string {
	return m.now().UTC().Format(time.RFC3339)
}

func (m *ObsidianMemory) ensureParentDir(rel, key string) error {
	dir := filepath.Dir(rel)
	if dir == "." {
		return nil
	}
	if err := m.root.MkdirAll(dir, privateDirMode); err != nil {
		return fmt.Errorf("obsidian: mkdir for %query: %w", key, err)
	}
	return nil
}

func (m *ObsidianMemory) commitNode(rel string, node contracts.Node) error {
	tmp := rel + ".tmp"
	if err := m.root.WriteFile(tmp, []byte(marshalNode(node)), privateFileMode); err != nil {
		return fmt.Errorf("obsidian: write %query: %w", node.Key, err)
	}
	if err := m.root.Rename(tmp, rel); err != nil {
		return fmt.Errorf("obsidian: commit %query: %w", node.Key, errors.Join(err, m.root.Remove(tmp)))
	}
	return nil
}

func (m *ObsidianMemory) load(key string) (contracts.Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadUnlocked(key)
}

// Record upserts a node: keyToRel is deterministic, so writing the same Key
// overwrites the same file (update in place, no duplicate).
func (m *ObsidianMemory) Record(ctx context.Context, node contracts.Node) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := m.flock(ctx)
	if err != nil {
		return err
	}
	err = m.recordUnlocked(node)
	return errors.Join(err, release())
}

// Recall loads the root node and breadth-first follows its links up to depth.
// It holds the in-process mutex and the cross-process lock for the whole walk so
// a concurrent writer can't make it see a node mid-update or miss a just-written
// one (every read goes through loadUnlocked under that lock).
func (m *ObsidianMemory) Recall(ctx context.Context, key string, depth int) (contracts.Subgraph, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := m.flock(ctx)
	if err != nil {
		return contracts.Subgraph{}, err
	}
	sg, err := m.recallUnlocked(ctx, key, depth)
	return sg, errors.Join(err, release())
}

type recallWalk struct {
	key   string
	sg    contracts.Subgraph
	seen  map[string]bool
	edges map[contracts.Link]bool
}

func (w *recallWalk) addEdge(link contracts.Link) {
	if w.edges[link] {
		return
	}
	w.edges[link] = true
	w.sg.Edges = append(w.sg.Edges, link)
}

func (m *ObsidianMemory) recallUnlocked(ctx context.Context, key string, depth int) (contracts.Subgraph, error) {
	root, err := m.loadUnlocked(key)
	if err != nil {
		return contracts.Subgraph{}, err
	}
	walk := &recallWalk{
		key:   key,
		sg:    contracts.Subgraph{Root: root},
		seen:  map[string]bool{key: true},
		edges: make(map[contracts.Link]bool),
	}
	frontier := []contracts.Node{root}
	for d := 0; d < depth && len(frontier) > 0; d++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return contracts.Subgraph{}, ctxErr
		}
		frontier, err = m.expandFrontier(walk, frontier)
		if err != nil {
			return contracts.Subgraph{}, err
		}
	}
	return walk.sg, nil
}

func (m *ObsidianMemory) expandFrontier(walk *recallWalk, frontier []contracts.Node) ([]contracts.Node, error) {
	var next []contracts.Node
	for _, node := range frontier {
		for _, link := range node.Links {
			child, admitted, err := m.visitLink(walk, link)
			if err != nil {
				return nil, err
			}
			if admitted {
				walk.sg.Nodes = append(walk.sg.Nodes, child)
				next = append(next, child)
			}
		}
	}
	return next, nil
}

func (m *ObsidianMemory) visitLink(walk *recallWalk, link contracts.Link) (contracts.Node, bool, error) {
	walk.addEdge(link)
	if walk.seen[link.To] {
		return contracts.Node{}, false, nil
	}
	walk.seen[link.To] = true
	child, err := m.loadUnlocked(link.To)
	if errors.Is(err, fs.ErrNotExist) {
		return contracts.Node{}, false, nil
	}
	if err != nil {
		return contracts.Node{}, false, fmt.Errorf("obsidian: recall %query: neighbor %query: %w", walk.key, link.To, err)
	}
	if child.Meta[contracts.MetaState] == contracts.StateArchived || child.Kind == contracts.KindTranscript {
		return contracts.Node{}, false, nil
	}
	return child, true, nil
}

// Links adds a typed edge from→to as a [[to|rel]] wikilink in the source's body.
// It is idempotent on the target: if an edge to `to` already exists it is left
// untouched (the vault document owns the relation label, since a human co-edits
// it), so re-linking with a different rel does not rewrite their prose.
func (m *ObsidianMemory) Links(ctx context.Context, from, target, rel string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := m.flock(ctx)
	if err != nil {
		return err
	}
	err = m.linksUnlocked(from, target, rel)
	return errors.Join(err, release())
}

func (m *ObsidianMemory) linksUnlocked(from, target, rel string) error {
	node, err := m.loadUnlocked(from)
	if err != nil {
		return err
	}
	for _, link := range node.Links {
		if link.To == target {
			return nil
		}
	}
	node.Links = append(node.Links, contracts.Link{To: target, Rel: rel})
	return m.recordUnlockedNoReload(node)
}

// Unlink removes the edge from→to (the inverse of Links): it drops every
// contracts.Link with To == to from the source node AND excises the matching
// [[to|rel]] wikilink text from its body, since the obsidian body is the source
// of truth for edges (marshalNode only appends, never removes, so dropping the
// Link alone would round-trip straight back). Idempotent: an absent edge is a
// no-op. Under the same lock as Links; a human co-owns the note.
func (m *ObsidianMemory) Unlink(ctx context.Context, from, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := m.flock(ctx)
	if err != nil {
		return err
	}
	err = m.unlinkUnlocked(from, target)
	return errors.Join(err, release())
}

func (m *ObsidianMemory) unlinkUnlocked(from, target string) error {
	node, err := m.loadUnlocked(from)
	if err != nil {
		return err
	}
	kept := node.Links[:0:0]
	found := false
	for _, link := range node.Links {
		if link.To == target {
			found = true
			continue
		}
		kept = append(kept, link)
	}
	if !found {
		return nil
	}
	node.Links = kept
	node.Body = exciseWikilinks(node.Body, target)
	return m.recordUnlockedNoReload(node)
}

// Close releases the vault lock and root handle.
func (m *ObsidianMemory) Close() error {
	if m.lockFile != nil {
		return errors.Join(m.lockFile.Close(), m.root.Close())
	}
	return m.root.Close()
}

// Search returns the nodes matching q; archived and raw nodes appear only when q asks for them.
func (m *ObsidianMemory) Search(ctx context.Context, query contracts.Query) ([]contracts.Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := m.flock(ctx)
	if err != nil {
		return nil, err
	}
	out, err := m.searchUnlocked(ctx, query)
	return out, errors.Join(err, release())
}

func (m *ObsidianMemory) searchUnlocked(ctx context.Context, query contracts.Query) ([]contracts.Node, error) {
	fsys := m.root.FS()
	var out []contracts.Node
	seen := make(map[string]struct{}, len(m.parseCache))
	err := fs.WalkDir(fsys, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if !entry.Type().IsRegular() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		seen[path] = struct{}{}
		node, err := m.nodeAtPath(fsys, path, entry)
		if err != nil {
			return err
		}
		if matchesQuery(node, query) {
			out = append(out, cloneNode(node))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("obsidian: search: %w", err)
	}
	m.pruneParseCache(seen)
	if query.Ranked {
		rankByRelevance(out, query.Text, m.now().UTC())
	}
	return limitNodes(out, query.Limit), nil
}

func (m *ObsidianMemory) nodeAtPath(fsys fs.FS, path string, entry fs.DirEntry) (contracts.Node, error) {
	info, err := entry.Info()
	if err != nil {
		return contracts.Node{}, err
	}
	return m.cachedNode(fsys, path, info)
}

func (m *ObsidianMemory) pruneParseCache(live map[string]struct{}) {
	for cachePath := range m.parseCache {
		if _, ok := live[cachePath]; !ok {
			delete(m.parseCache, cachePath)
		}
	}
}

func limitNodes(out []contracts.Node, limit int) []contracts.Node {
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

func (m *ObsidianMemory) cachedNode(fsys fs.FS, path string, info fs.FileInfo) (contracts.Node, error) {
	if c, ok := m.parseCache[path]; ok && c.size == info.Size() && c.mod.Equal(info.ModTime()) {
		return c.node, nil
	}
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return contracts.Node{}, err
	}
	node := unmarshalNode(strings.TrimSuffix(path, ".md"), data)
	m.parseCache[path] = cachedNode{mod: info.ModTime(), size: info.Size(), node: node}
	return node, nil
}

func rankByRelevance(out []contracts.Node, text string, now time.Time) {
	scores := make(map[string]float64, len(out))
	for _, node := range out {
		if s, textHit := contracts.Score(text, now, node); textHit {
			scores[node.Key] = s
			continue
		}
		scores[node.Key] = -1 + recencyRank(node, now)*recencyWeight
	}
	sort.SliceStable(out, func(i, j int) bool { return scores[out[i].Key] > scores[out[j].Key] })
}

func matchesQuery(node contracts.Node, query contracts.Query) bool {
	return visibleInQuery(node, query) && matchesKinds(node, query.Kinds) && matchesText(node, query.Text) && matchesTags(node, query.Tags)
}

func visibleInQuery(node contracts.Node, query contracts.Query) bool {
	if node.Meta[contracts.MetaState] == contracts.StateArchived && !query.IncludeArchived {
		return false
	}
	return node.Kind != contracts.KindTranscript || query.IncludeRaw
}

func matchesKinds(node contracts.Node, kinds []contracts.NodeKind) bool {
	return len(kinds) == 0 || slices.Contains(kinds, node.Kind)
}

func matchesText(node contracts.Node, text string) bool {
	if text == "" {
		return true
	}
	hay := strings.ToLower(node.Title + "\n" + node.Body)
	return strings.Contains(hay, strings.ToLower(text))
}

func matchesTags(node contracts.Node, tags []string) bool {
	for _, want := range tags {
		if !hasTag(node, want) {
			return false
		}
	}
	return true
}

func hasTag(node contracts.Node, want string) bool {
	wanted := strings.ToLower(want)
	domain := strings.TrimSpace(strings.ToLower(node.Meta["domain"]))
	if domain != "" && wanted == domain {
		return true
	}
	for _, t := range strings.Split(strings.ToLower(node.Meta["tags"]), ",") {
		if strings.TrimSpace(t) == wanted {
			return true
		}
	}
	return false
}

func recencyRank(node contracts.Node, now time.Time) float64 {
	captured, err := time.Parse(time.RFC3339, node.Meta["capturedAt"])
	if err != nil {
		return 0
	}
	ageDays := now.Sub(captured).Hours() / hoursPerDay
	if ageDays < 0 {
		ageDays = 0
	}
	return math.Exp(-math.Ln2 * ageDays / recencyHalfLifeDays)
}
