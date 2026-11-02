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
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("obsidian: open vault root: %w", err)
	}
	lf, err := r.OpenFile(lockName, os.O_CREATE|os.O_RDWR, privateFileMode)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("obsidian: open vault lock: %w", err), r.Close())
	}
	return &ObsidianMemory{root: r, lockFile: lf, now: time.Now, parseCache: map[string]cachedNode{}}, nil
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

func cloneNode(n contracts.Node) contracts.Node {
	n.Meta = maps.Clone(n.Meta)
	n.Links = slices.Clone(n.Links)
	return n
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
		return contracts.Node{}, fmt.Errorf("obsidian: load %q: %w", key, err)
	}
	return unmarshalNode(key, data), nil
}

func (m *ObsidianMemory) recordUnlocked(n contracts.Node) error {
	if m.budget > 0 && n.Kind != contracts.KindTranscript {
		if r := utf8.RuneCountInString(n.Body); r > m.budget {
			return &contracts.BudgetError{Key: n.Key, Runes: r, Limit: m.budget}
		}
	}
	return m.writeNode(n, true)
}

func (m *ObsidianMemory) recordUnlockedNoReload(n contracts.Node) error {
	return m.writeNode(n, false)
}

func (m *ObsidianMemory) writeNode(n contracts.Node, reloadPrior bool) error {
	if err := validateNodeKeys(n); err != nil {
		return err
	}
	n = m.stampedNode(n, reloadPrior)
	rel := keyToRel(n.Key)
	if err := m.ensureParentDir(rel, n.Key); err != nil {
		return err
	}
	return m.commitNode(rel, n)
}

func validateNodeKeys(n contracts.Node) error {
	if err := validKey(n.Key); err != nil {
		return err
	}
	for _, l := range n.Links {
		if err := validKey(l.To); err != nil {
			return fmt.Errorf("obsidian: node %q links to an unusable key: %w", n.Key, err)
		}
		if i := strings.IndexAny(l.Rel, wikilinkMeta); i >= 0 {
			return fmt.Errorf("%w: node %q link relation %q contains %q, which a wikilink cannot carry", ErrInvalidLink, n.Key, l.Rel, l.Rel[i:i+1])
		}
	}
	return nil
}

func (m *ObsidianMemory) stampedNode(n contracts.Node, reloadPrior bool) contracts.Node {
	n.Meta = maps.Clone(n.Meta)
	if n.Meta == nil {
		n.Meta = map[string]string{}
	}
	if n.Meta["capturedAt"] == "" {
		n.Meta["capturedAt"] = m.capturedAtFor(n.Key, reloadPrior)
	}
	if n.Meta[contracts.MetaLastSeen] == "" {
		n.Meta[contracts.MetaLastSeen] = m.nowStamp()
	}
	return n
}

func (m *ObsidianMemory) capturedAtFor(key string, reloadPrior bool) string {
	at := m.nowStamp()
	if reloadPrior {
		if existing, err := m.loadUnlocked(key); err == nil {
			if prior := existing.Meta["capturedAt"]; prior != "" {
				return prior
			}
		}
	}
	return at
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
		return fmt.Errorf("obsidian: mkdir for %q: %w", key, err)
	}
	return nil
}

func (m *ObsidianMemory) commitNode(rel string, n contracts.Node) error {
	tmp := rel + ".tmp"
	if err := m.root.WriteFile(tmp, []byte(marshalNode(n)), privateFileMode); err != nil {
		return fmt.Errorf("obsidian: write %q: %w", n.Key, err)
	}
	if err := m.root.Rename(tmp, rel); err != nil {
		return fmt.Errorf("obsidian: commit %q: %w", n.Key, errors.Join(err, m.root.Remove(tmp)))
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
func (m *ObsidianMemory) Record(ctx context.Context, n contracts.Node) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := m.flock(ctx)
	if err != nil {
		return err
	}
	err = m.recordUnlocked(n)
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

func (w *recallWalk) addEdge(l contracts.Link) {
	if w.edges[l] {
		return
	}
	w.edges[l] = true
	w.sg.Edges = append(w.sg.Edges, l)
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
		edges: map[contracts.Link]bool{},
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
	for _, n := range frontier {
		for _, l := range n.Links {
			child, admitted, err := m.visitLink(walk, l)
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

func (m *ObsidianMemory) visitLink(walk *recallWalk, l contracts.Link) (contracts.Node, bool, error) {
	walk.addEdge(l)
	if walk.seen[l.To] {
		return contracts.Node{}, false, nil
	}
	walk.seen[l.To] = true
	child, err := m.loadUnlocked(l.To)
	if errors.Is(err, fs.ErrNotExist) {
		return contracts.Node{}, false, nil
	}
	if err != nil {
		return contracts.Node{}, false, fmt.Errorf("obsidian: recall %q: neighbor %q: %w", walk.key, l.To, err)
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
func (m *ObsidianMemory) Links(ctx context.Context, from, to, rel string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := m.flock(ctx)
	if err != nil {
		return err
	}
	err = m.linksUnlocked(from, to, rel)
	return errors.Join(err, release())
}

func (m *ObsidianMemory) linksUnlocked(from, to, rel string) error {
	n, err := m.loadUnlocked(from)
	if err != nil {
		return err
	}
	for _, l := range n.Links {
		if l.To == to {
			return nil
		}
	}
	n.Links = append(n.Links, contracts.Link{To: to, Rel: rel})
	return m.recordUnlockedNoReload(n)
}

// Unlink removes the edge from→to (the inverse of Links): it drops every
// contracts.Link with To == to from the source node AND excises the matching
// [[to|rel]] wikilink text from its body, since the obsidian body is the source
// of truth for edges (marshalNode only appends, never removes, so dropping the
// Link alone would round-trip straight back). Idempotent: an absent edge is a
// no-op. Under the same lock as Links; a human co-owns the note.
func (m *ObsidianMemory) Unlink(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := m.flock(ctx)
	if err != nil {
		return err
	}
	err = m.unlinkUnlocked(from, to)
	return errors.Join(err, release())
}

func (m *ObsidianMemory) unlinkUnlocked(from, to string) error {
	n, err := m.loadUnlocked(from)
	if err != nil {
		return err
	}
	kept := n.Links[:0:0]
	found := false
	for _, l := range n.Links {
		if l.To == to {
			found = true
			continue
		}
		kept = append(kept, l)
	}
	if !found {
		return nil
	}
	n.Links = kept
	n.Body = exciseWikilinks(n.Body, to)
	return m.recordUnlockedNoReload(n)
}

// Close releases the vault lock and root handle.
func (m *ObsidianMemory) Close() error {
	if m.lockFile != nil {
		return errors.Join(m.lockFile.Close(), m.root.Close())
	}
	return m.root.Close()
}

func (m *ObsidianMemory) Search(ctx context.Context, q contracts.Query) ([]contracts.Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := m.flock(ctx)
	if err != nil {
		return nil, err
	}
	out, err := m.searchUnlocked(ctx, q)
	return out, errors.Join(err, release())
}

func (m *ObsidianMemory) searchUnlocked(ctx context.Context, q contracts.Query) ([]contracts.Node, error) {
	fsys := m.root.FS()
	var out []contracts.Node
	seen := make(map[string]struct{}, len(m.parseCache))
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		seen[path] = struct{}{}
		n, err := m.nodeAtPath(fsys, path, d)
		if err != nil {
			return err
		}
		if matchesQuery(n, q) {
			out = append(out, cloneNode(n))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("obsidian: search: %w", err)
	}
	m.pruneParseCache(seen)
	if q.Ranked {
		rankByRelevance(out, q.Text, m.now().UTC())
	}
	return limitNodes(out, q.Limit), nil
}

func (m *ObsidianMemory) nodeAtPath(fsys fs.FS, path string, d fs.DirEntry) (contracts.Node, error) {
	info, err := d.Info()
	if err != nil {
		return contracts.Node{}, err
	}
	return m.cachedNode(fsys, path, info)
}

func (m *ObsidianMemory) pruneParseCache(live map[string]struct{}) {
	for p := range m.parseCache {
		if _, ok := live[p]; !ok {
			delete(m.parseCache, p)
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
	n := unmarshalNode(strings.TrimSuffix(path, ".md"), data)
	m.parseCache[path] = cachedNode{mod: info.ModTime(), size: info.Size(), node: n}
	return n, nil
}

func rankByRelevance(out []contracts.Node, text string, now time.Time) {
	scores := make(map[string]float64, len(out))
	for _, n := range out {
		if s, textHit := contracts.Score(text, now, n); textHit {
			scores[n.Key] = s
			continue
		}
		scores[n.Key] = -1 + recencyRank(n, now)*recencyWeight
	}
	sort.SliceStable(out, func(i, j int) bool { return scores[out[i].Key] > scores[out[j].Key] })
}

func matchesQuery(n contracts.Node, q contracts.Query) bool {
	return visibleInQuery(n, q) && matchesKinds(n, q.Kinds) && matchesText(n, q.Text) && matchesTags(n, q.Tags)
}

func visibleInQuery(n contracts.Node, q contracts.Query) bool {
	if n.Meta[contracts.MetaState] == contracts.StateArchived && !q.IncludeArchived {
		return false
	}
	return n.Kind != contracts.KindTranscript || q.IncludeRaw
}

func matchesKinds(n contracts.Node, kinds []contracts.NodeKind) bool {
	return len(kinds) == 0 || slices.Contains(kinds, n.Kind)
}

func matchesText(n contracts.Node, text string) bool {
	if text == "" {
		return true
	}
	hay := strings.ToLower(n.Title + "\n" + n.Body)
	return strings.Contains(hay, strings.ToLower(text))
}

func matchesTags(n contracts.Node, tags []string) bool {
	for _, want := range tags {
		if !hasTag(n, want) {
			return false
		}
	}
	return true
}

func hasTag(n contracts.Node, want string) bool {
	w := strings.ToLower(want)
	domain := strings.TrimSpace(strings.ToLower(n.Meta["domain"]))
	if domain != "" && w == domain {
		return true
	}
	for _, t := range strings.Split(strings.ToLower(n.Meta["tags"]), ",") {
		if strings.TrimSpace(t) == w {
			return true
		}
	}
	return false
}

func recencyRank(n contracts.Node, now time.Time) float64 {
	at, err := time.Parse(time.RFC3339, n.Meta["capturedAt"])
	if err != nil {
		return 0
	}
	ageDays := now.Sub(at).Hours() / hoursPerDay
	if ageDays < 0 {
		ageDays = 0
	}
	return math.Exp(-math.Ln2 * ageDays / recencyHalfLifeDays)
}
