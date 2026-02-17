package config

import (
	"encoding/json"
	"os"
	"sync"
)

type Config struct {
	mu sync.RWMutex

	CommandPrefix string `json:"command_prefix"`
	Port          string `json:"port"`

	// Cookie values
	Cookies map[string]string `json:"cookies"`
}

func New() *Config {
	return &Config{
		CommandPrefix: "!",
		Port:/* default */ "8080",
		Cookies: make(map[string]string),
	}
}

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return New(), nil
		}
		return nil, err
	}
	defer f.Close()

	var cfg Config
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Cookies == nil {
		cfg.Cookies = make(map[string]string)
	}
	if cfg.CommandPrefix == "" {
		cfg.CommandPrefix = "!"
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	return &cfg, nil
}

func (c *Config) Save(path string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(c)
}

func (c *Config) Update(newCfg *Config) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.CommandPrefix = newCfg.CommandPrefix
	c.Port = newCfg.Port
	c.Cookies = newCfg.Cookies
}
