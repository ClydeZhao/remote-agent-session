package localstate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

func digest(value []byte) string {
	h := sha256.Sum256(value)
	return hex.EncodeToString(h[:])
}

func (s *Store) SaveCredential(server string, credential []byte) error {
	if server == "" || len(credential) == 0 || len(credential) > 65536 {
		return errors.New("invalid credential")
	}
	return s.locked(func() error { return s.write("credential-"+digest([]byte(server))+".json", credential) })
}

func (s *Store) Credential(server string) (credential []byte, err error) {
	err = s.locked(func() error {
		if e := s.read("credential-"+digest([]byte(server))+".json", &credential); e != nil {
			return errors.New("login credential unavailable; log in again")
		}
		if len(credential) == 0 || len(credential) > 65536 {
			return errors.New("login credential unavailable; log in again")
		}
		return nil
	})
	return
}

func (s *Store) ForgetCredential(server string) error {
	return s.locked(func() error {
		err := os.Remove(filepath.Join(s.dir, "credential-"+digest([]byte(server))+".json"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("cannot remove local credential")
		}
		return s.syncDir()
	})
}
