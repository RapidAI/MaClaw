package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func listDirNamesLimited(dir string, limit int) ([]string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, os.ErrNotExist
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 40
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && name != ".maclaw" && name != ".codegraph" && name != ".github" {
			continue
		}
		if e.IsDir() {
			names = append(names, name+"/")
		} else {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) > limit {
		names = names[:limit]
	}
	// Prefer relative-looking names only.
	for i := range names {
		names[i] = filepath.ToSlash(names[i])
	}
	return names, nil
}
