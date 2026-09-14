package place

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateDirRejectsLinksAndWritableParents(t *testing.T) {
	root := t.TempDir()
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
	if PrivateDir(filepath.Join(loose, "backup")) == nil {
		t.Fatal("writable ancestor accepted")
	}
	good := filepath.Join(root, "good")
	if err := PrivateDir(good); err != nil {
		t.Fatal(err)
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
