package localstate

import (
	"errors"
	"os"
)

var ErrRelayRunning = errors.New("session bridge is already running")

func (s *Store) SaveRelayIntent(identity string) error {
	return s.locked(func() error { return s.write("relay-intent.json", identity) })
}
func (s *Store) ReplaceRelayIntent(current, replacement string) (replaced bool, err error) {
	err = s.locked(func() error {
		var intent string
		if err := s.read("relay-intent.json", &intent); err != nil {
			return err
		}
		if intent != current {
			return nil
		}
		if err := s.write("relay-intent.json", replacement); err != nil {
			return err
		}
		replaced = true
		return nil
	})
	return replaced, err
}

func (s *Store) SaveRelayOwner(identity string) error {
	return s.locked(func() error { return s.write("relay-owner.json", identity) })
}
func (s *Store) relayIdentity(name string) (value string, err error) {
	err = s.locked(func() error {
		e := s.read(name, &value)
		if errors.Is(e, os.ErrNotExist) {
			return nil
		}
		return e
	})
	return
}
func (s *Store) RelayIntent() (string, error) { return s.relayIdentity("relay-intent.json") }
func (s *Store) RelayOwner() (string, error)  { return s.relayIdentity("relay-owner.json") }

type RelayHandoff struct {
	RuntimeKind     string `json:"runtime_kind,omitempty"`
	RuntimeInstance string `json:"runtime_instance,omitempty"`
	Runtime         string `json:"runtime,omitempty"`
	Server          string `json:"server"`
	UserID          string `json:"user_id"`
	NativeSession   string `json:"native_session"`
	ClientID        string `json:"client_id"`
}

func (s *Store) RelayHandoffs() (v []RelayHandoff, err error) {
	err = s.locked(func() error {
		e := s.read("relay-handoff.json", &v)
		if errors.Is(e, os.ErrNotExist) {
			return nil
		}
		return e
	})
	return
}
func (s *Store) AddRelayHandoffs(items []RelayHandoff) error {
	return s.locked(func() error {
		var existing []RelayHandoff
		if e := s.read("relay-handoff.json", &existing); e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		for _, item := range items {
			found := false
			for _, old := range existing {
				if old == item {
					found = true
					break
				}
			}
			if !found {
				existing = append(existing, item)
			}
		}
		if len(existing) > 100 {
			return errors.New("too many pending relay handoffs")
		}
		return s.write("relay-handoff.json", existing)
	})
}
func (s *Store) FinishRelayHandoff(item RelayHandoff) error {
	return s.locked(func() error {
		var existing []RelayHandoff
		if e := s.read("relay-handoff.json", &existing); e != nil {
			return e
		}
		kept := existing[:0]
		for _, old := range existing {
			if old != item {
				kept = append(kept, old)
			}
		}
		return s.write("relay-handoff.json", kept)
	})
}

type RelayReadiness struct {
	Controller      string `json:"controller"`
	Generation      int64  `json:"generation"`
	RuntimeKind     string `json:"runtime_kind"`
	RuntimeInstance string `json:"runtime_instance"`
	NativeSession   string `json:"native_session"`
}

func (s *Store) SaveRelayReadiness(v RelayReadiness) error {
	return s.locked(func() error {
		var owner, intent string
		if err := s.read("relay-owner.json", &owner); err != nil {
			return err
		}
		if err := s.read("relay-intent.json", &intent); err != nil {
			return err
		}
		if owner != v.Controller || intent != v.Controller {
			return errors.New("relay ownership changed before readiness confirmation")
		}
		return s.write("relay-ready.json", v)
	})
}
func (s *Store) RelayReadiness() (v RelayReadiness, err error) {
	err = s.locked(func() error { return s.read("relay-ready.json", &v) })
	return
}
