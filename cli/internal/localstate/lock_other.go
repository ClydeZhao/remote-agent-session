//go:build !darwin && !linux && !windows

package localstate

import "errors"

func supportedPlatform() error {
	return errors.New("protected local storage is not implemented on this platform yet")
}
func (s *Store) locked(fn func() error) error { return supportedPlatform() }
