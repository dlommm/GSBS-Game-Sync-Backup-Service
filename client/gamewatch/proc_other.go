//go:build !linux && !windows && !darwin

package gamewatch

// NewDetector returns a stub detector on platforms without process listing;
// game-aware sync degrades to always-off there.
func NewDetector() Detector {
	return unsupportedDetector{}
}

type unsupportedDetector struct{}

func (unsupportedDetector) Snapshot() ([]ProcessInfo, error) {
	return nil, ErrUnsupported
}
