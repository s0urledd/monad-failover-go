package place

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// PrivateDir refuses links, foreign owners and writable ancestors before
// using a directory for secrets. Root-owned sticky /tmp is a safe ancestor
// of an exclusively created, owned test directory, never a secret directory.
func PrivateDir(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if abs == string(filepath.Separator) {
		return fmt.Errorf("refusing filesystem root as a private directory")
	}
	cur := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(abs, cur), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if os.IsNotExist(err) {
			if err = os.Mkdir(cur, 0700); err != nil {
				return err
			}
			fi, err = os.Lstat(cur)
		}
		if err != nil {
			return err
		}
		if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s must be a real directory, not a symlink", cur)
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok || (st.Uid != 0 && int(st.Uid) != os.Geteuid()) {
			return fmt.Errorf("%s has an untrusted owner; use a private directory owned by the operator running this tool", cur)
		}
		stickyAncestor := cur != abs && fi.Mode()&os.ModeSticky != 0 && st.Uid == 0
		if fi.Mode().Perm()&0022 != 0 && !stickyAncestor {
			return fmt.Errorf("%s is writable by other users; refusing to store secrets", cur)
		}
		if cur == abs && int(st.Uid) != os.Geteuid() {
			return fmt.Errorf("%s must be owned by uid %d", cur, os.Geteuid())
		}
	}
	f, err := os.OpenFile(abs, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Chmod(0700)
}

func SyncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// CheckMetadata is checked before unmasking, including on resume.
func CheckMetadata(path string, uid, gid int) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.Mode().IsRegular() || !ok || int(st.Uid) != uid || int(st.Gid) != gid || fi.Mode().Perm() != 0600 {
		return fmt.Errorf("%s has unexpected ownership or permissions; services remain stopped", path)
	}
	return nil
}
