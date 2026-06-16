package obsidian

import (
	"regexp"
	"sort"
	"strings"

	"github.com/Herrscherd/herrscher-contracts"
)

var wikilinkRe = regexp.MustCompile(`\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)

const liensHeader = "## Liens"

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
		b.WriteString(k + ": " + frontValue(n.Meta[k]) + "\n")
	}
	b.WriteString("---\n")

	body := n.Body
	present := map[string]bool{}
	for _, m := range wikilinkRe.FindAllStringSubmatch(body, -1) {
		present[m[1]] = true
	}
	var missing []string
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
		if !strings.Contains(body, liensHeader) {
			b.WriteString("\n" + liensHeader + "\n")
		}
		b.WriteString(strings.Join(missing, "\n") + "\n")
	}
	return b.String()
}

func unmarshalNode(key string, data []byte) contracts.Node {
	n := contracts.Node{Key: key, Meta: map[string]string{}}
	s := string(data)
	body := s

	if strings.HasPrefix(s, "---\n") {
		if end := strings.Index(s[4:], "\n---"); end >= 0 {
			front := s[4 : 4+end]
			body = strings.TrimPrefix(s[4+end+4:], "\n")
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
	}
	n.Body = body
	for _, m := range wikilinkRe.FindAllStringSubmatch(body, -1) {
		n.Links = append(n.Links, contracts.Link{To: m[1], Rel: m[2]})
	}
	return n
}
