package obsidian

import (
	"regexp"
	"sort"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

var wikilinkRe = regexp.MustCompile(`\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)

const liensHeader = "## Liens"

const (
	relContains  = "contains"
	relBelongsTo = "belongs-to"
)

var liensHeaderRe = regexp.MustCompile(`(?m)^## Liens[ \t]*$`)

func frontValue(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}

func frontKey(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(frontValue(s), ":", ""))
}

func marshalNode(n contracts.Node) string {
	return frontMatter(n) + withMissingLinks(n.Body, n.Links)
}

func frontMatter(n contracts.Node) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("type: " + string(n.Kind) + "\n")
	if n.Title != "" {
		b.WriteString("title: " + frontValue(n.Title) + "\n")
	}
	for _, k := range writableMetaKeys(n.Meta) {
		b.WriteString(frontKey(k) + ": " + frontValue(n.Meta[k]) + "\n")
	}
	b.WriteString("---\n")
	return b.String()
}

func writableMetaKeys(meta map[string]string) []string {
	keys := make([]string, 0, len(meta))
	for k := range meta {
		if fk := frontKey(k); fk == "" || fk == "type" || fk == "title" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func withMissingLinks(body string, links []contracts.Link) string {
	missing := missingLinkLines(body, links)
	if len(missing) == 0 {
		return body
	}
	var b strings.Builder
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteString("\n")
	}
	if !liensHeaderRe.MatchString(body) {
		b.WriteString("\n" + liensHeader + "\n")
	}
	b.WriteString(strings.Join(missing, "\n") + "\n")
	return b.String()
}

func missingLinkLines(body string, links []contracts.Link) []string {
	present := map[string]bool{}
	for _, m := range wikilinkRe.FindAllStringSubmatch(body, -1) {
		present[m[1]] = true
	}
	missing := make([]string, 0, len(links))
	for _, l := range links {
		if present[l.To] {
			continue
		}
		tag := l.To
		if l.Rel != "" {
			tag += "|" + l.Rel
		}
		missing = append(missing, "- [["+tag+"]]")
		present[l.To] = true
	}
	return missing
}

func exciseWikilinks(body, to string) string {
	q := regexp.QuoteMeta(to)
	tok := `\[\[` + q + `(?:\|[^\]]*)?\]\]`
	targetTok := regexp.MustCompile(tok)
	bulletOnly := regexp.MustCompile(`^\s*- ` + tok + `\s*$`)
	spaceThenTok := regexp.MustCompile(`[ \t]` + tok)
	tokThenSpace := regexp.MustCompile(tok + `[ \t]`)

	lines := strings.Split(body, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if !targetTok.MatchString(line) {
			kept = append(kept, line)
			continue
		}
		if bulletOnly.MatchString(line) {
			continue
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

	split, ok := splitFrontmatter(s)
	front, body := split.front, split.body
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

type splitNote struct {
	front, body string
}

func splitFrontmatter(s string) (splitNote, bool) {
	if !strings.HasPrefix(s, "---\n") {
		return splitNote{body: s}, false
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
				return splitNote{front: rest[:off]}, true
			}
			return splitNote{front: rest[:off], body: rest[off+nl+1:]}, true
		}
		if nl < 0 {
			break
		}
		off += nl + 1
	}
	return splitNote{body: s}, false
}
