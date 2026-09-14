package webgen

import (
	"fmt"
	"strings"
)

// lineDiff is a minimal line diff of ref → got, as zero-context hunks:
//
//	@@ -<ref line>,<count> +<got line>,<count> @@
//	-<ref line>
//	+<got line>
//
// It trims the common prefix and suffix, then takes an LCS of the middle, which
// keeps it cheap on large files that differ in a few lines. "" means identical.
func lineDiff(ref, got string) string {
	a, b := strings.Split(ref, "\n"), strings.Split(got, "\n")
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	return hunks(a[pre:len(a)-suf], b[pre:len(b)-suf], pre)
}

type edit struct {
	op   byte // ' ', '-', '+'
	text string
}

func lcsEdits(a, b []string) []edit {
	n, m := len(a), len(b)
	l := make([][]int, n+1)
	for i := range l {
		l[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				l[i][j] = l[i+1][j+1] + 1
			} else {
				l[i][j] = max(l[i+1][j], l[i][j+1])
			}
		}
	}
	var out []edit
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			out = append(out, edit{' ', a[i]})
			i, j = i+1, j+1
		case i < n && (j == m || l[i+1][j] >= l[i][j+1]):
			out = append(out, edit{'-', a[i]})
			i++
		default:
			out = append(out, edit{'+', b[j]})
			j++
		}
	}
	return out
}

func hunks(a, b []string, offset int) string {
	var sb strings.Builder
	ai, bi := offset, offset
	var cur []edit
	start := [2]int{}
	flush := func() {
		if len(cur) == 0 {
			return
		}
		var dels, adds int
		for _, e := range cur {
			if e.op == '-' {
				dels++
			} else {
				adds++
			}
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", start[0]+1, dels, start[1]+1, adds)
		for _, e := range cur {
			sb.WriteString(string(e.op) + e.text + "\n")
		}
		cur = nil
	}
	for _, e := range lcsEdits(a, b) {
		if e.op == ' ' {
			flush()
			ai, bi = ai+1, bi+1
			continue
		}
		if len(cur) == 0 {
			start = [2]int{ai, bi}
		}
		cur = append(cur, e)
		if e.op == '-' {
			ai++
		} else {
			bi++
		}
	}
	flush()
	return sb.String()
}
