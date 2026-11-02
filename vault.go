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

func frontValue(text string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(text)
}

func frontKey(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(frontValue(text), ":", ""))
}

func marshalNode(node contracts.Node) string {
	return frontMatter(node) + withMissingLinks(node.Body, node.Links)
}

func frontMatter(node contracts.Node) string {
	lines := []string{"---", "type: " + string(node.Kind)}
	if node.Title != "" {
		lines = append(lines, "title: "+frontValue(node.Title))
	}
	for _, metaKey := range writableMetaKeys(node.Meta) {
		lines = append(lines, frontKey(metaKey)+": "+frontValue(node.Meta[metaKey]))
	}
	lines = append(lines, "---")
	return strings.Join(lines, "\n") + "\n"
}

func writableMetaKeys(meta map[string]string) []string {
	keys := make([]string, 0, len(meta))
	for metaKey := range meta {
		if fk := frontKey(metaKey); fk == "" || fk == "type" || fk == "title" {
			continue
		}
		keys = append(keys, metaKey)
	}
	sort.Strings(keys)
	return keys
}

func withMissingLinks(body string, links []contracts.Link) string {
	missing := missingLinkLines(body, links)
	if len(missing) == 0 {
		return body
	}
	head := body
	if !strings.HasSuffix(body, "\n") {
		head += "\n"
	}
	if !liensHeaderRe.MatchString(body) {
		head += "\n" + liensHeader + "\n"
	}
	return head + strings.Join(missing, "\n") + "\n"
}

func missingLinkLines(body string, links []contracts.Link) []string {
	present := make(map[string]bool)
	for _, m := range wikilinkRe.FindAllStringSubmatch(body, -1) {
		present[m[1]] = true
	}
	missing := make([]string, 0, len(links))
	for _, link := range links {
		if present[link.To] {
			continue
		}
		tag := link.To
		if link.Rel != "" {
			tag += "|" + link.Rel
		}
		missing = append(missing, "- [["+tag+"]]")
		present[link.To] = true
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
	node := contracts.Node{Key: key, Meta: make(map[string]string)}
	text := string(data)

	split, ok := splitFrontmatter(text)
	front, body := split.front, split.body
	if ok {
		for _, line := range strings.Split(front, "\n") {
			field, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			field, value = strings.TrimSpace(field), strings.TrimSpace(value)
			switch field {
			case "type":
				node.Kind = contracts.NodeKind(value)
			case "title":
				node.Title = value
			default:
				node.Meta[field] = value
			}
		}
	}
	node.Body = body
	matches := wikilinkRe.FindAllStringSubmatch(body, -1)
	node.Links = make([]contracts.Link, 0, len(matches))
	for _, m := range matches {
		node.Links = append(node.Links, contracts.Link{To: m[1], Rel: m[2]})
	}
	return node
}

type splitNote struct {
	front, body string
}

func splitFrontmatter(text string) (splitNote, bool) {
	if !strings.HasPrefix(text, "---\n") {
		return splitNote{body: text}, false
	}
	rest := text[4:]
	for off := 0; off <= len(rest); {
		line := rest[off:]
		lineEnd := strings.IndexByte(line, '\n')
		cur := line
		if lineEnd >= 0 {
			cur = line[:lineEnd]
		}
		if cur == "---" {
			if lineEnd < 0 {
				return splitNote{front: rest[:off]}, true
			}
			return splitNote{front: rest[:off], body: rest[off+lineEnd+1:]}, true
		}
		if lineEnd < 0 {
			break
		}
		off += lineEnd + 1
	}
	return splitNote{body: text}, false
}
