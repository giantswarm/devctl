package updater

import (
	"os"
	"path/filepath"
	"time"

	"github.com/giantswarm/microerror"
	"sigs.k8s.io/yaml"
)

const (
	cacheValidationDuration = 1 * time.Hour
	cacheFileName           = "selfupdate.yaml"
	cacheFileMode           = 0600
)

type cache struct {
	LastUpdate    time.Time `json:"last_update"`
	LatestVersion string    `json:"latest_version"`
}

func (c *cache) IsExpired() bool {
	expectedExpiration := c.LastUpdate.Add(cacheValidationDuration)

	return expectedExpiration.Before(time.Now().UTC())
}

func (c *cache) Persist(cacheDir string) error {
	c.LastUpdate = time.Now().UTC()

	serialized, err := yaml.Marshal(c)
	if err != nil {
		return microerror.Mask(err)
	}

	err = os.MkdirAll(cacheDir, 0700)
	if err != nil {
		return microerror.Mask(err)
	}

	out := filepath.Join(cacheDir, cacheFileName)

	// Readers and other devctl processes must see a complete document. Write
	// to a separate file in the same directory and publish it with one rename.
	tmp, err := os.CreateTemp(cacheDir, ".selfupdate-*.yaml")
	if err != nil {
		return microerror.Mask(err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(serialized); err != nil {
		_ = tmp.Close()
		return microerror.Mask(err)
	}
	if err := tmp.Close(); err != nil {
		return microerror.Mask(err)
	}
	if err := os.Rename(tmp.Name(), out); err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (c *cache) Restore(fromPath string) error {
	in := filepath.Join(fromPath, cacheFileName)

	in = filepath.Clean(in)
	serialized, err := os.ReadFile(in)
	if err != nil {
		return microerror.Mask(err)
	}

	err = yaml.Unmarshal(serialized, c)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
