package place

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// PrivateDir refuses links, foreign owners and writable ancestors before
// using a directory for secrets, creating what is missing at 0700. Root-owned
// sticky /tmp is a safe ancestor of an exclusively created, owned test
// directory, never a secret directory.
func PrivateDir(path string) error {
	abs, err := walkPrivate(path, true)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(abs, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Chmod(0700)
}

// PrivateDirCheck applies the same rules without creating or changing
// anything, for the dry run: components that do not exist yet would be
// created by the run itself at 0700 and are not a finding.
func PrivateDirCheck(path string) error {
	_, err := walkPrivate(path, false)
	return err
}

func walkPrivate(path string, create bool) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if abs == string(filepath.Separator) {
		return "", fmt.Errorf("refusing filesystem root as a private directory")
	}
	cur := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(abs, cur), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if os.IsNotExist(err) {
			if !create {
				return abs, nil
			}
			if err = os.Mkdir(cur, 0700); err != nil {
				return "", err
			}
			fi, err = os.Lstat(cur)
		}
		if err != nil {
			return "", err
		}
		if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%s must be a real directory, not a symlink", cur)
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok || (st.Uid != 0 && int(st.Uid) != os.Geteuid()) {
			return "", fmt.Errorf("%s is owned by uid %d, not root; a directory another account owns could be swapped under the run. Use a root-owned location", cur, st.Uid)
		}
		stickyAncestor := cur != abs && fi.Mode()&os.ModeSticky != 0 && st.Uid == 0
		if fi.Mode().Perm()&0022 != 0 && !stickyAncestor {
			return "", fmt.Errorf("%s is writable by other users (mode %o); another account could swap a directory under it. Use a location whose parents are root-owned and not group- or world-writable, such as the default under /var/lib/monad-failover",
				cur, fi.Mode().Perm())
		}
		if cur == abs && int(st.Uid) != os.Geteuid() {
			return "", fmt.Errorf("%s must be owned by uid %d", cur, os.Geteuid())
		}
	}
	return abs, nil
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
