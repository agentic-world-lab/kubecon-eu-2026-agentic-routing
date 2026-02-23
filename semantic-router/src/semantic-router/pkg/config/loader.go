package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v2"
)

var (
	config     *RouterConfig
	configOnce sync.Once
	configErr  error
	configMu   sync.RWMutex

	configUpdateCh chan *RouterConfig
	configUpdateMu sync.Mutex
)

// Load loads the configuration from the specified YAML file once and caches it globally.
func Load(configPath string) (*RouterConfig, error) {
	configOnce.Do(func() {
		cfg, err := Parse(configPath)
		if err != nil {
			configErr = err
			return
		}
		configMu.Lock()
		config = cfg
		configMu.Unlock()
	})
	if configErr != nil {
		return nil, configErr
	}
	configMu.RLock()
	defer configMu.RUnlock()
	return config, nil
}

// Parse parses the YAML config file without touching the global cache.
func Parse(configPath string) (*RouterConfig, error) {
	resolved, _ := filepath.EvalSymlinks(configPath)
	if resolved == "" {
		resolved = configPath
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg := &RouterConfig{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return cfg, nil
}

// Replace replaces the globally cached config.
func Replace(newCfg *RouterConfig) {
	configMu.Lock()
	config = newCfg
	configErr = nil
	configMu.Unlock()

	configUpdateMu.Lock()
	if configUpdateCh != nil {
		select {
		case configUpdateCh <- newCfg:
		default:
		}
	}
	configUpdateMu.Unlock()
}

// Get returns the current configuration.
func Get() *RouterConfig {
	configMu.RLock()
	defer configMu.RUnlock()
	return config
}

// WatchConfigUpdates returns a channel that receives config updates.
func WatchConfigUpdates() <-chan *RouterConfig {
	configUpdateMu.Lock()
	defer configUpdateMu.Unlock()

	if configUpdateCh == nil {
		configUpdateCh = make(chan *RouterConfig, 1)
	}
	return configUpdateCh
}
