package promote

import (
	"regexp"
	"strconv"

	"github.com/s0urledd/monad-failover-go/internal/foundation"
)

var (
	seqInputRe = regexp.MustCompile(`^[1-9][0-9]{0,15}$`)
	// noteCharsRe strips whatever the state file's shape for a snapshot
	// note does not accept, so the note can be shown again on resume.
	noteCharsRe = regexp.MustCompile(`[^A-Za-z0-9 ,.'_-]`)
)

// ValidSeq reports whether s is a sequence number the tool accepts: decimal
// without a leading zero, within the range a JSON double keeps intact. The
// command line checks --seq with it before anything runs.
func ValidSeq(s string) bool {
	if !seqInputRe.MatchString(s) {
		return false
	}
	n, err := strconv.ParseUint(s, 10, 64)
	return err == nil && n <= foundation.SeqSaneMax
}
