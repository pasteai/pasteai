package main

import (
	"strings"
	"testing"
)

func TestDefaultDBPath(t *testing.T) {
	p := defaultDBPath()
	if !strings.HasSuffix(p, ".db") {
		t.Errorf("defaultDBPath = %q, expected .db extension", p)
	}
	if !strings.Contains(p, "pasteai") {
		t.Errorf("defaultDBPath = %q, expected 'pasteai' in path", p)
	}
}

func TestPrintUsage(t *testing.T) {
	printUsage()
}
