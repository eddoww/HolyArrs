package sites

import (
	"bytes"
	"html"
	"strings"
)

// A small bounded HTML tree for the sites that answer with pages. It reads the
// page in one forward pass with the same tag reader as the Addic7ed adapter,
// skips comments, scripts and styles, and closes the tags HTML lets a page
// leave open (li, p, td, th, tr, option). Broken nesting is taken as it comes,
// nothing is ever run, and the node count and depth are capped so a hostile
// page stays cheap.

// Caps on one page tree.
const (
	pageMaxNodes = 200000
	pageMaxDepth = 256
)

type node struct {
	tag    string // lower case; "" for a text node
	raw    []byte // the attribute bytes of an element
	text   string // a text node's text, with entities decoded
	parent *node
	kids   []*node
}

// voidTags never have content or a closing tag.
var voidTags = map[string]bool{"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "param": true, "source": true, "track": true, "wbr": true}

// openCloses lists, for a tag, the open tags it closes and the tags that stop
// the search for them, as in a ul for li. The outermost open tag it closes
// wins, so a new tr closes the open td and its tr.
var openCloses = map[string]struct{ closes, stops []string }{
	"li":     {[]string{"li"}, []string{"ul", "ol"}},
	"p":      {[]string{"p"}, []string{"div", "td", "li"}},
	"td":     {[]string{"td", "th"}, []string{"tr", "table"}},
	"th":     {[]string{"td", "th"}, []string{"tr", "table"}},
	"tr":     {[]string{"tr", "td", "th"}, []string{"table", "tbody", "thead"}},
	"option": {[]string{"option"}, []string{"select"}},
}

// parsePage builds the tree of a page.
func parsePage(b []byte) *node {
	root := &node{tag: "#root"}
	cur, depth, count := root, 0, 0
	add := func(n *node) bool {
		if count >= pageMaxNodes {
			return false
		}
		count++
		n.parent = cur
		cur.kids = append(cur.kids, n)
		return true
	}
	i := 0
	for i < len(b) && count < pageMaxNodes {
		if b[i] != '<' {
			j := bytes.IndexByte(b[i:], '<')
			if j < 0 {
				j = len(b) - i
			}
			add(&node{text: html.UnescapeString(string(b[i : i+j]))})
			i += j
			continue
		}
		if bytes.HasPrefix(b[i:], []byte("<!--")) {
			j := bytes.Index(b[i+4:], []byte("-->"))
			if j < 0 {
				break
			}
			i += 4 + j + 3
			continue
		}
		j := tagEnd(b[i+1:])
		if j < 0 {
			break
		}
		inner := b[i+1 : i+1+j]
		i += j + 2
		end := len(inner) > 0 && inner[0] == '/'
		if end {
			inner = inner[1:]
		}
		n := 0
		for n < len(inner) && (inner[n] >= 'a' && inner[n] <= 'z' || inner[n] >= 'A' && inner[n] <= 'Z' || inner[n] >= '0' && inner[n] <= '9') {
			n++
		}
		if n == 0 {
			continue // doctype, processing instruction or a stray angle bracket
		}
		tag := strings.ToLower(string(inner[:n]))
		if end {
			for up, d := cur, depth; up != root; up, d = up.parent, d-1 {
				if up.tag == tag {
					cur, depth = up.parent, d-1
					break
				}
			}
			continue
		}
		if tag == "script" || tag == "style" {
			k := indexClosingTag(b[i:], tag)
			if k < 0 {
				break
			}
			i += k
			continue
		}
		if rule, ok := openCloses[tag]; ok {
			to, toDepth := cur, depth
		find:
			for up, d := cur, depth; up != root; up, d = up.parent, d-1 {
				for _, s := range rule.stops {
					if up.tag == s {
						break find
					}
				}
				for _, c := range rule.closes {
					if up.tag == c {
						to, toDepth = up.parent, d-1
					}
				}
			}
			cur, depth = to, toDepth
		}
		el := &node{tag: tag, raw: inner[n:]}
		if !add(el) {
			break
		}
		selfClosed := bytes.HasSuffix(bytes.TrimRight(inner, " \t\r\n"), []byte("/"))
		if !voidTags[tag] && !selfClosed && depth < pageMaxDepth {
			cur, depth = el, depth+1
		}
	}
	return root
}

// attr returns the value of one attribute.
func (n *node) attr(name string) string { return attr(n.raw, name) }

// hasClass reports whether the element carries the class c.
func (n *node) hasClass(c string) bool {
	for _, f := range strings.Fields(n.attr("class")) {
		if strings.EqualFold(f, c) {
			return true
		}
	}
	return false
}

// all returns the elements under n, in page order, for which match is true.
func (n *node) all(match func(*node) bool) []*node {
	var out []*node
	var walk func(*node)
	walk = func(x *node) {
		for _, k := range x.kids {
			if k.tag != "" && match(k) {
				out = append(out, k)
			}
			walk(k)
		}
	}
	walk(n)
	return out
}

// first returns the first element under n for which match is true, or nil.
func (n *node) first(match func(*node) bool) *node {
	if all := n.all(match); len(all) > 0 {
		return all[0]
	}
	return nil
}

// tagged matches elements by tag, and by class too when class is not empty.
func tagged(tag, class string) func(*node) bool {
	return func(n *node) bool { return n.tag == tag && (class == "" || n.hasClass(class)) }
}

// textOf returns the text under n with its white space collapsed.
func (n *node) textOf() string {
	return strings.Join(strings.Fields(strings.Join(n.lines(), " ")), " ")
}

// lines returns the text under n split at line breaks and block elements, each
// line with its white space collapsed and empty lines left out.
func (n *node) lines() []string {
	var out []string
	var sb strings.Builder
	flush := func() {
		if s := strings.Join(strings.Fields(sb.String()), " "); s != "" {
			out = append(out, s)
		}
		sb.Reset()
	}
	var walk func(*node)
	walk = func(x *node) {
		for _, k := range x.kids {
			switch k.tag {
			case "":
				if sb.Len() < 4096 {
					sb.WriteString(k.text)
				}
			case "br", "p", "div", "li", "tr", "td", "h1", "h2", "h3", "h4", "h5", "h6":
				flush()
				walk(k)
				flush()
			default:
				walk(k)
			}
		}
	}
	walk(n)
	flush()
	return out
}
