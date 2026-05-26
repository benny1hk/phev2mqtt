package web

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v2"
)

const (
	configKeyUsername     = "web_username"
	configKeyPasswordHash = "web_password_hash"
	defaultUsername       = "admin"
	defaultPassword       = "admin"
)

// credentials are loaded from viper at server startup and updated when the
// password is changed via the API. A mutex guards reads/writes so the auth
// middleware and change-password handler can co-exist safely.
type credentials struct {
	mu                  sync.RWMutex
	username            string
	passwordHash        string
	mustChangePassword  bool
}

func loadCredentials() (*credentials, error) {
	c := &credentials{
		username:     viper.GetString(configKeyUsername),
		passwordHash: viper.GetString(configKeyPasswordHash),
	}

	if c.username == "" {
		c.username = defaultUsername
	}

	if c.passwordHash == "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(defaultPassword), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("failed to hash default password: %w", err)
		}
		c.passwordHash = string(hash)
		c.mustChangePassword = true

		viper.Set(configKeyUsername, c.username)
		viper.Set(configKeyPasswordHash, c.passwordHash)
		if err := persistConfig(map[string]interface{}{
			configKeyUsername:     c.username,
			configKeyPasswordHash: c.passwordHash,
		}); err != nil {
			log.Warnf("Could not persist seeded web credentials: %v", err)
		}
		log.Warnf("SECURITY: web UI seeded with default credentials %s/%s — change immediately via the UI", defaultUsername, defaultPassword)
	}

	return c, nil
}

func (c *credentials) get() (string, string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.username, c.passwordHash, c.mustChangePassword
}

func (c *credentials) verify(username, password string) bool {
	c.mu.RLock()
	u, h := c.username, c.passwordHash
	c.mu.RUnlock()
	if username != u {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(h), []byte(password)) == nil
}

func (c *credentials) setPassword(newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	c.mu.Lock()
	c.passwordHash = string(hash)
	c.mustChangePassword = false
	c.mu.Unlock()

	viper.Set(configKeyPasswordHash, string(hash))
	return persistConfig(map[string]interface{}{
		configKeyPasswordHash: string(hash),
	})
}

// persistConfig rewrites the viper-managed YAML file with the given keys
// updated, preserving any other keys already present. The file is written
// atomically (temp + rename). If no config file is in use yet, one is created
// at the default location ($HOME/.phev2mqtt.yaml).
func persistConfig(updates map[string]interface{}) error {
	path := viper.ConfigFileUsed()
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("could not determine home directory: %w", err)
		}
		path = filepath.Join(home, ".phev2mqtt.yaml")
	}

	existing := map[string]interface{}{}
	if data, err := ioutil.ReadFile(path); err == nil {
		if len(data) > 0 {
			if err := yaml.Unmarshal(data, &existing); err != nil {
				return fmt.Errorf("could not parse existing config %s: %w", path, err)
			}
			if existing == nil {
				existing = map[string]interface{}{}
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("could not read config %s: %w", path, err)
	}

	for k, v := range updates {
		existing[k] = v
	}

	out, err := yaml.Marshal(existing)
	if err != nil {
		return fmt.Errorf("could not marshal config: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := ioutil.TempFile(dir, ".phev2mqtt-config-*.yaml")
	if err != nil {
		return fmt.Errorf("could not create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("could not write temp config: %w", err)
	}
	if err := tmp.Chmod(0600); err != nil {
		log.Debugf("could not chmod %s: %v", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("could not close temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("could not rename temp config to %s: %w", path, err)
	}

	return nil
}
