package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSplitTarget(t *testing.T) {
	dir := t.TempDir()
	hashName := filepath.Join(dir, "notes#1.md")
	if err := os.WriteFile(hashName, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct{ target, path, section string }{
		{"PLAN.md", "PLAN.md", ""},
		{"docs/design.md#Rollout plan", "docs/design.md", "Rollout plan"},
		{"docs/design.md#rollout-plan", "docs/design.md", "rollout-plan"},
		{hashName, hashName, ""},
		{hashName + "#Intro", hashName, "Intro"},
	}
	for _, c := range cases {
		path, section := splitTarget(c.target)
		if path != c.path || section != c.section {
			t.Errorf("splitTarget(%q) = %q, %q; want %q, %q",
				c.target, path, section, c.path, c.section)
		}
	}
}
