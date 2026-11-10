package sites

import (
	"strings"
	"testing"
)

func TestParsePageClosesOpenTags(t *testing.T) {
	root := parsePage([]byte(`<ul><li>one<li>two<ul><li>inner</ul><li class="x y">three</ul>
		<table><tr><td>a<td>b<tr><td>c</table><p>first<p>second<br>line<!-- <li>hidden</li> --><script><li>no</li></script>`))
	var lis []string
	for _, li := range root.all(tagged("li", "")) {
		lis = append(lis, li.textOf())
	}
	if strings.Join(lis, "|") != "one|two inner|inner|three" {
		t.Errorf("li texts %q", lis)
	}
	if root.first(tagged("li", "y")) == nil {
		t.Error("class not found")
	}
	if rows := root.all(tagged("tr", "")); len(rows) != 2 || len(rows[0].all(tagged("td", ""))) != 2 {
		t.Errorf("rows %d", len(rows))
	}
	ps := root.all(tagged("p", ""))
	if len(ps) != 2 || strings.Join(ps[1].lines(), "|") != "second|line" {
		t.Errorf("p %d %q", len(ps), ps[1].lines())
	}
}

func TestParsePageStaysBounded(t *testing.T) {
	deep := strings.Repeat("<div>", 100000) + "text" + strings.Repeat("</div>", 10)
	if root := parsePage([]byte(deep)); root == nil {
		t.Fatal("no root")
	}
	many := strings.Repeat("<b>x</b>", pageMaxNodes)
	root := parsePage([]byte(many))
	if n := len(root.all(func(*node) bool { return true })); n > pageMaxNodes {
		t.Fatalf("%d nodes", n)
	}
}
