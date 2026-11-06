package main

import (
	"os"
	"path/filepath"
)

// Reuse the legacy vault in place: its keychain entry is bound to its path.
// Renaming or copying it would create a different master-key lookup.
func dataDirectory(root, override, legacyOverride string) (string, error) {
	if override != "" {
		return override, nil
	}
	if legacyOverride != "" {
		return legacyOverride, nil
	}
	for _, name := range []string{"YunTools", "AliyunTools"} {
		dir := filepath.Join(root, name)
		if _, err := os.Stat(filepath.Join(dir, "vault.enc")); err == nil {
			return dir, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	return filepath.Join(root, "YunTools"), nil
}
