package ui

import (
	"os"
	"testing"
	"zfs-file-history/internal/zfs"
)

func TestMain(m *testing.M) {
	// all allowed: tests never run sudo, and the permissions are not offered at the start
	allowedSudoersRules = func() map[string]bool {
		allowed := map[string]bool{}
		for _, rule := range zfs.SudoersRules {
			allowed[rule.Id] = true
		}
		return allowed
	}
	os.Exit(m.Run())
}
