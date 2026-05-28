package util

import (
	"os"
	"os/user"
	path2 "path"
	"strconv"
	"syscall"
)

func FileExists(path string) bool {
	statSnap, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false
	}

	return statSnap != nil
}

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

func UnixPerm(m os.FileMode) (p uint32) {
	p = uint32(m.Perm())
	if m&os.ModeSetuid != 0 {
		p |= 04000
	}
	if m&os.ModeSetgid != 0 {
		p |= 02000
	}
	if m&os.ModeSticky != 0 {
		p |= 01000
	}
	return p
}

func UnixPermissions(m os.FileMode) uint32 {
	return UnixPerm(m)
}

func UnixPermSymbolic(m os.FileMode) string {
	perms := []rune("----------")

	switch {
	case m.IsDir():
		perms[0] = 'd'
	case m&os.ModeSymlink != 0:
		perms[0] = 'l'
	case m&os.ModeNamedPipe != 0:
		perms[0] = 'p'
	case m&os.ModeSocket != 0:
		perms[0] = 's'
	case m&os.ModeDevice != 0 && m&os.ModeCharDevice != 0:
		perms[0] = 'c'
	case m&os.ModeDevice != 0:
		perms[0] = 'b'
	}

	if m&0400 != 0 {
		perms[1] = 'r'
	}
	if m&0200 != 0 {
		perms[2] = 'w'
	}
	if m&0100 != 0 {
		perms[3] = 'x'
	}
	if m&0040 != 0 {
		perms[4] = 'r'
	}
	if m&0020 != 0 {
		perms[5] = 'w'
	}
	if m&0010 != 0 {
		perms[6] = 'x'
	}
	if m&0004 != 0 {
		perms[7] = 'r'
	}
	if m&0002 != 0 {
		perms[8] = 'w'
	}
	if m&0001 != 0 {
		perms[9] = 'x'
	}

	if m&os.ModeSetuid != 0 {
		if perms[3] == 'x' {
			perms[3] = 's'
		} else {
			perms[3] = 'S'
		}
	}
	if m&os.ModeSetgid != 0 {
		if perms[6] == 'x' {
			perms[6] = 's'
		} else {
			perms[6] = 'S'
		}
	}
	if m&os.ModeSticky != 0 {
		if perms[9] == 'x' {
			perms[9] = 't'
		} else {
			perms[9] = 'T'
		}
	}

	return string(perms)
}

func UnixOwnerIDs(stat os.FileInfo) (uid uint32, gid uint32, ok bool) {
	if stat == nil || stat.Sys() == nil {
		return 0, 0, false
	}

	statT, typeOk := stat.Sys().(*syscall.Stat_t)
	if !typeOk || statT == nil {
		return 0, 0, false
	}

	return statT.Uid, statT.Gid, true
}

func LookupUserName(uid uint32) (string, error) {
	u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

func LookupGroupName(gid uint32) (string, error) {
	g, err := user.LookupGroupId(strconv.FormatUint(uint64(gid), 10))
	if err != nil {
		return "", err
	}
	return g.Name, nil
}
