package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenamedAppPreservesExistingVaultAndHonorsOverrides(t *testing.T) {
	root := t.TempDir()
	check := func(override, legacyOverride, want string) {
		t.Helper()
		got, err := dataDirectory(root, override, legacyOverride)
		if err != nil || got != want {
			t.Fatalf("got %q, %v; want %q", got, err, want)
		}
	}
	check("", "", filepath.Join(root, "YunTools"))
	legacy := filepath.Join(root, "AliyunTools")
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "vault.enc"), []byte("encrypted fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	check("", "", legacy)
	check("", "/legacy-override", "/legacy-override")
	check("/new-override", "/legacy-override", "/new-override")
	newDir := filepath.Join(root, "YunTools")
	if err := os.MkdirAll(newDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "vault.enc"), []byte("new encrypted fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	check("", "", newDir)
}
