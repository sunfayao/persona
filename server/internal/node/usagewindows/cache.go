package usagewindows

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/sunfayao/persona/server/internal/managerconf"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Cache stores normalized node samples under their source keys.
type Cache struct {
	path string
}

func New(dir string) *Cache {
	return &Cache{path: filepath.Join(dir, "usage", "node-cache.json")}
}

// Load discards expired samples while preserving their original sampling times.
func (c *Cache) Load(now time.Time, maxAge time.Duration) (map[string]proto.UsageNodeAccount, error) {
	var accounts map[string]proto.UsageNodeAccount
	data, err := os.ReadFile(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &accounts); err != nil {
		return nil, err
	}
	for key, account := range accounts {
		at := account.SampledAt
		if at.IsZero() || now.Sub(at) > maxAge || at.After(now.Add(5*time.Minute)) || len(account.Windows) == 0 {
			delete(accounts, key)
		}
	}
	return accounts, nil
}

// Save atomically replaces the snapshot or removes it after collection is disabled.
func (c *Cache) Save(accounts map[string]proto.UsageNodeAccount) error {
	if len(accounts) == 0 {
		if err := os.Remove(c.path); !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	data, err := json.Marshal(accounts)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	return managerconf.WriteFileAtomic(c.path, data, 0o600)
}
