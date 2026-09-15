package place

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateDirRejectsLinksAndWritableParents(t *testing.T) {
	root := t.TempDir()
	os.Chmod(root, 0o700)
	target := filepath.Join(root, "target")
	os.Mkdir(target, 0700)
	link := filepath.Join(root, "link")
	os.Symlink(target, link)
	if PrivateDir(link) == nil {
		t.Fatal("symlink accepted")
	}
	loose := filepath.Join(root, "loose")
	os.Mkdir(loose, 0700)
	os.Chmod(loose, 0777)
	err := PrivateDir(filepath.Join(loose, "backup"))
	if err == nil || !strings.Contains(err.Error(), loose+" is writable by other users (mode 777)") || !strings.Contains(err.Error(), "/var/lib/monad-failover") {
		t.Fatalf("writable ancestor: %v", err)
	}
	if _, err := os.Stat(filepath.Join(loose, "backup")); err == nil {
		t.Fatal("directory created under a refused ancestor")
	}
	good := filepath.Join(root, "good")
	if err := PrivateDir(good); err != nil {
		t.Fatal(err)
	}
}

// The check form finds the same problems without creating anything, and a
// path that does not exist yet under a sound ancestor is not a finding.
func TestPrivateDirCheckCreatesNothing(t *testing.T) {
	root := t.TempDir()
	os.Chmod(root, 0o700)
	loose := filepath.Join(root, "loose")
	os.Mkdir(loose, 0700)
	os.Chmod(loose, 0775)
	err := PrivateDirCheck(filepath.Join(loose, "monad", "backup"))
	if err == nil || !strings.Contains(err.Error(), loose+" is writable by other users (mode 775)") {
		t.Fatalf("group-writable ancestor: %v", err)
	}
	if err := PrivateDirCheck(filepath.Join(root, "not", "yet", "there")); err != nil {
		t.Fatalf("missing path under a sound ancestor: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "not")); err == nil {
		t.Fatal("check created a directory")
	}
	link := filepath.Join(root, "link")
	os.Symlink(loose, link)
	if PrivateDirCheck(filepath.Join(link, "x")) == nil {
		t.Fatal("symlink ancestor accepted")
	}
}
func TestPrivateDirForeignOwnerRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root ownership test")
	}
	root := t.TempDir()
	os.Chown(root, 65534, 65534)
	defer os.Chown(root, 0, 0)
	if PrivateDir(root) == nil {
		t.Fatal("foreign owner accepted")
	}
}
func TestPlacedOwnershipRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root ownership test")
	}
	staged, live, want := setup(t, "new")
	if _, err := VerifiedOwned(staged, live, want, "key", "backup", 65534, 65534); err != nil {
		t.Fatal(err)
	}
	if err := CheckMetadata(live, 65534, 65534); err != nil {
		t.Fatal(err)
	}
}
