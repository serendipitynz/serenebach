package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExtractAssetsWritesAllFiles(t *testing.T) {
	outDir := t.TempDir()
	runExtractAssets([]string{"-out", outDir})

	want := []string{
		"admin.css",
		"admin.js",
		"sb_logo_dark.svg",
		"sb_logo_light.svg",
		"sb_logo_gray.svg",
		"favicon.png",
		"MANIFEST",
	}
	for _, name := range want {
		full := filepath.Join(outDir, name)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
		}
	}

	// MANIFEST should contain a version string.
	manifest, err := os.ReadFile(filepath.Join(outDir, "MANIFEST"))
	if err != nil {
		t.Fatalf("read MANIFEST: %v", err)
	}
	if !strings.Contains(string(manifest), "serenebach") {
		t.Errorf("MANIFEST missing version marker, got: %s", manifest)
	}
}

func TestRunExtractAssetsOverwritesExisting(t *testing.T) {
	outDir := t.TempDir()
	// First run.
	runExtractAssets([]string{"-out", outDir})
	// Second run should succeed (overwrite).
	runExtractAssets([]string{"-out", outDir})
}
