package localstate

import (
	"os"
	"time"
)

type UpdateCache struct {
	Source    string    `json:"source"`
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest,omitempty"`
}

func (s *Store) UpdateCache() (v UpdateCache, err error) {
	err = s.locked(func() error { return s.read("update-cache.json", &v) })
	return
}
func (s *Store) SaveUpdateCache(v UpdateCache) error {
	return s.locked(func() error { return s.write("update-cache.json", v) })
}

func (s *Store) ClaimUpdateRefresh(source string) bool {
	claimed := false
	_ = s.locked(func() error {
		var v UpdateCache
		e := s.read("update-cache.json", &v)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if e == nil && v.Source == source && time.Since(v.CheckedAt) < 24*time.Hour {
			return nil
		}
		claimed = true
		return s.write("update-cache.json", UpdateCache{Source: source, CheckedAt: time.Now().UTC()})
	})
	return claimed
}
