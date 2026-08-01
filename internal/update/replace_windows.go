//go:build windows

package update

type replacementInput struct {
	Target        string
	Candidate     string
	CandidateSHA  string
	OldSHA        string
	OldSize       int64
	RemoteVersion string
}

func applyReplacement(input replacementInput) (Status, error) {
	return prepareDeferredReplacement(input, defaultDeferredPrepOptions())
}
