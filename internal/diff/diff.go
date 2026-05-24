package diff

import (
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// Unified returns a unified diff string comparing a (old) to b (new).
func Unified(labelA, labelB, a, b string) string {
	d := difflib.UnifiedDiff{
		A:        difflib.SplitLines(a),
		B:        difflib.SplitLines(b),
		FromFile: labelA,
		ToFile:   labelB,
		Context:  3,
	}
	text, _ := difflib.GetUnifiedDiffString(d)
	return text
}

// CountLines computes the number of added and removed lines between a and b.
func CountLines(a, b string) (added, removed int) {
	text := Unified("", "", a, b)
	for _, line := range strings.Split(text, "\n") {
		if len(line) == 0 {
			continue
		}
		switch line[0] {
		case '+':
			if !strings.HasPrefix(line, "+++") {
				added++
			}
		case '-':
			if !strings.HasPrefix(line, "---") {
				removed++
			}
		}
	}
	return
}
