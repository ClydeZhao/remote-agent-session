package localstate

import (
	"errors"
	"os"
)

func (s *Store) RelayEndpoint(instance string) (directory string, err error) {
	err = s.locked(func() error {
		var endpoints map[string]string
		if e := s.read("relay-endpoints.json", &endpoints); e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		directory = endpoints[instance]
		return nil
	})
	return
}

func (s *Store) SaveRelayEndpoint(instance, directory string) error {
	return s.locked(func() error {
		endpoints := map[string]string{}
		if e := s.read("relay-endpoints.json", &endpoints); e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if endpoints == nil {
			return errors.New("invalid relay endpoint state")
		}
		if old := endpoints[instance]; old != "" && old != directory {
			return errors.New("relay endpoint identity conflict")
		}
		if instance == "" || directory == "" {
			return errors.New("relay endpoint identity is required")
		}
		endpoints[instance] = directory
		return s.write("relay-endpoints.json", endpoints)
	})
}
