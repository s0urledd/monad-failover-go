package promote

import (
	"github.com/s0urledd/monad-failover-go/internal/systemd"
	"github.com/s0urledd/monad-failover-go/internal/ui"
)

// unitProblems reports what would stop the cutover's mask step: a unit
// systemd does not know, or a unit file under /etc/systemd/system, where
// `systemctl mask` cannot place its symlink. Both are found here, before
// anything is stopped, rather than at cutover with the old validator
// already down.
func unitProblems() ([]string, error) {
	var problems []string
	for _, u := range systemd.Units {
		path, err := systemd.UnitFile(u)
		if err != nil {
			return nil, err
		}
		switch {
		case path == "":
			problems = append(problems, u+": not found by systemd")
		case !systemd.Maskable(path):
			problems = append(problems, u+": unit file is "+path+"; systemctl mask cannot override a unit in /etc/systemd/system")
		}
	}
	return problems, nil
}

// unitAdvice is what to do about a unit file the cutover cannot mask.
var unitAdvice = []string{
	"The cutover masks the units so a reboot mid-swap cannot start a half-swapped node.",
	"Move a unit file in /etc/systemd/system to /usr/local/lib/systemd/system/,",
	"run systemctl daemon-reload, and re-run. Nothing has been changed.",
}

func (r *Run) checkUnits() error {
	r.c.Step("SYSTEMD UNITS")
	problems, err := unitProblems()
	if err != nil {
		return ui.Die("Cannot query the monad units: " + err.Error())
	}
	if len(problems) > 0 {
		return ui.Die("The monad units cannot be masked; refusing to prepare a cutover.", append(problems, unitAdvice...)...)
	}
	r.c.OK("monad-bft, monad-execution, monad-rpc found; unit files can be masked")
	return nil
}
