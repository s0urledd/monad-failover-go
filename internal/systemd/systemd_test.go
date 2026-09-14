package systemd

import "testing"

func TestMaskable(t *testing.T) {
	for path, want := range map[string]bool{
		"/lib/systemd/system/monad-bft.service":           true,
		"/usr/lib/systemd/system/monad-bft.service":       true,
		"/usr/local/lib/systemd/system/monad-bft.service": true,
		"/etc/systemd/system/monad-bft.service":           false,
		"":                                                false,
	} {
		if got := Maskable(path); got != want {
			t.Errorf("Maskable(%q) = %v, want %v", path, got, want)
		}
	}
}
