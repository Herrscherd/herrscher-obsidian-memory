package obsidian

import (
	"regexp"
	"sort"
	"strings"

	"github.com/Herrscherd/herrscher-contracts"
)

var wikilinkRe = regexp.MustCompile(`\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)

const liensHeader = "## Liens"

// liensHeaderRe matches the "## Liens" trailing links section as a full line, so
// the header is not suppressed by an incidental "## Liens" substring elsewhere in
// the body (prose, a quote, a code block).
var liensHeaderRe = regexp.MustCompile(`(?m)^## Liens[ \t]*$`)

// frontValue keeps a frontmatter scalar on a single line so a value can never
// inject extra keys or a closing "---" fence.
func frontValue(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}

func marshalNode(n contracts.Node) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("type: " + string(n.Kind) + "\n")
	if n.Title != "" {
		b.WriteString("title: " + frontValue(n.Title) + "\n")
	}
	keys := make([]string, 0, len(n.Meta))
	for k := range n.Meta {
		if k == "type" || k == "title" {
			continue // reserved for Kind/Title — never let Meta shadow them
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(frontValue(k) + ": " + frontValue(n.Meta[k]) + "\n")
	}
	b.WriteString("---\n")

	body := n.Body
	present := map[string]bool{}
	for _, m := range wikilinkRe.FindAllStringSubmatch(body, -1) {
		present[m[1]] = true
	}
	missing := make([]string, 0, len(n.Links))
	for _, l := range n.Links {
		if !present[l.To] {
			tag := l.To
			if l.Rel != "" {
				tag += "|" + l.Rel
			}
			missing = append(missing, "- [["+tag+"]]")
			present[l.To] = true
		}
	}
	b.WriteString(body)
	if len(missing) > 0 {
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
		if !liensHeaderRe.MatchString(body) {
			b.WriteString("\n" + liensHeader + "\n")
		}
		b.WriteString(strings.Join(missing, "\n") + "\n")
	}
	return b.String()
}

// exciseWikilinks removes every wikilink whose target key is `to` from body,
// touching ONLY lines that actually contain such a link so the rest of the
// human-co-owned note is byte-preserved (no body-wide reflow). A line that is a
// pure managed bullet ("- [[to|rel]]") is dropped whole; an inline token is
// removed together with one adjacent space so no gap or leading/trailing space
// is left behind. Other wikilinks, the "## Liens" header, code blocks, tables,
// and prose on untouched lines are left exactly as they were.
func exciseWikilinks(body, to string) string {
	q := regexp.QuoteMeta(to)
	tok := `\[\[` + q + `(?:\|[^\]]*)?\]\]`         // a wikilink whose target is `to`
	targetTok := regexp.MustCompile(tok)            // bare token
	bulletOnly := regexp.MustCompile(`^\s*- ` + tok + `\s*$`)
	spaceThenTok := regexp.MustCompile(`[ \t]` + tok) // " [[to]]" — drop token + its leading space
	tokThenSpace := regexp.MustCompile(tok + `[ \t]`) // "[[to]] " — line-leading fallback

	lines := strings.Split(body, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if !targetTok.MatchString(line) {
			kept = append(kept, line) // no edge to `to` here — preserve verbatim
			continue
		}
		if bulletOnly.MatchString(line) {
			continue // pure "- [[to|rel]]" bullet — remove the whole line
		}
		line = spaceThenTok.ReplaceAllString(line, "")
		line = tokThenSpace.ReplaceAllString(line, "")
		line = targetTok.ReplaceAllString(line, "")
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func unmarshalNode(key string, data []byte) contracts.Node {
	n := contracts.Node{Key: key, Meta: map[string]string{}}
	s := string(data)

	front, body, ok := splitFrontmatter(s)
	if ok {
		for _, line := range strings.Split(front, "\n") {
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			switch k {
			case "type":
				n.Kind = contracts.NodeKind(v)
			case "title":
				n.Title = v
			default:
				n.Meta[k] = v
			}
		}
	}
	n.Body = body
	matches := wikilinkRe.FindAllStringSubmatch(body, -1)
	n.Links = make([]contracts.Link, 0, len(matches))
	for _, m := range matches {
		n.Links = append(n.Links, contracts.Link{To: m[1], Rel: m[2]})
	}
	return n
}

// splitFrontmatter separates a leading YAML frontmatter block from the body. The
// opening and closing fences must each be a standalone "---" line: a file that
// merely begins with a markdown thematic break, or whose only "---" sits mid-line,
// is treated as pure body so hand-authored notes are never mis-parsed. When no
// closing fence is found ok is false and the whole input is returned as body.
func splitFrontmatter(s string) (front, body string, ok bool) {
	if !strings.HasPrefix(s, "---\n") {
		return "", s, false
	}
	rest := s[4:]
	for off := 0; off <= len(rest); {
		line := rest[off:]
		nl := strings.IndexByte(line, '\n')
		cur := line
		if nl >= 0 {
			cur = line[:nl]
		}
		if cur == "---" {
			if nl < 0 {
				return rest[:off], "", true
			}
			return rest[:off], rest[off+nl+1:], true
		}
		if nl < 0 {
			break
		}
		off += nl + 1
	}
	return "", s, false
}
