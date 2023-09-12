package util

import (
	"os"
	path2 "path"
)

func ListFilesIn(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return []string{}, err
	}

	var result []string
	for _, entry := range entries {
		result = append(result, path2.Join(path, entry.Name()))
	}

	return result, nil
}
