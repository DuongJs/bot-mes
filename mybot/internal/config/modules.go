package config

import (
	"os"
	"path/filepath"
)

// DiscoverModules scans a "modules" directory adjacent to the given config file.
// Each subdirectory name is treated as an enabled module.
// Returns nil if the modules directory does not exist, letting the caller fall
// back to the modules map inside the config file.
func DiscoverModules(configPath string) map[string]bool {
	dir := filepath.Dir(configPath)
	modulesDir := filepath.Join(dir, "modules")

	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		return nil
	}

	modules := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() {
			modules[entry.Name()] = true
		}
	}
	return modules
}
