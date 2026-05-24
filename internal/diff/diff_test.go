package diff_test

import (
	"strings"
	"testing"

	"github.com/pasteai/pasteai/internal/diff"
)

func TestUnifiedIdentical(t *testing.T) {
	out := diff.Unified("a", "b", "same\n", "same\n")
	if out != "" {
		t.Errorf("expected empty diff for identical input, got %q", out)
	}
}

func TestUnifiedAddition(t *testing.T) {
	out := diff.Unified("old", "new", "line1\n", "line1\nnew line\n")
	if !strings.Contains(out, "+new line") {
		t.Errorf("expected +new line in diff, got:\n%s", out)
	}
}

func TestUnifiedDeletion(t *testing.T) {
	out := diff.Unified("old", "new", "line1\nremoved\n", "line1\n")
	if !strings.Contains(out, "-removed") {
		t.Errorf("expected -removed in diff, got:\n%s", out)
	}
}

func TestUnifiedLabels(t *testing.T) {
	out := diff.Unified("revision 1", "current", "a\n", "b\n")
	if !strings.Contains(out, "--- revision 1") {
		t.Errorf("expected --- revision 1 header, got:\n%s", out)
	}
	if !strings.Contains(out, "+++ current") {
		t.Errorf("expected +++ current header, got:\n%s", out)
	}
}

func TestCountLinesIdentical(t *testing.T) {
	added, removed := diff.CountLines("same\n", "same\n")
	if added != 0 || removed != 0 {
		t.Errorf("want 0/0, got %d/%d", added, removed)
	}
}

func TestCountLinesAddition(t *testing.T) {
	added, removed := diff.CountLines("a\n", "a\nb\n")
	if added != 1 || removed != 0 {
		t.Errorf("want added=1 removed=0, got %d/%d", added, removed)
	}
}

func TestCountLinesDeletion(t *testing.T) {
	added, removed := diff.CountLines("a\nb\n", "a\n")
	if added != 0 || removed != 1 {
		t.Errorf("want added=0 removed=1, got %d/%d", added, removed)
	}
}

func TestCountLinesMixed(t *testing.T) {
	added, removed := diff.CountLines("a\nb\nc\n", "a\nX\nY\nc\n")
	if added != 2 || removed != 1 {
		t.Errorf("want added=2 removed=1, got %d/%d", added, removed)
	}
}
