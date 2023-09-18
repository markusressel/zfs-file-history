package util

import (
	"os"
	path2 "path"
)

func ListFilesIn(path string) (result []string, err error) {
	if _, err = os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return result, nil
		} else {
			return result, err
		}
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return result, err
	}

	for _, entry := range entries {
		result = append(result, path2.Join(path, entry.Name()))
	}

	return result, nil
}
