package config

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

type Config struct {
	mu sync.RWMutex

	CommandPrefix string `json:"command_prefix"`
	Port          string `json:"port"`

	// Raw cookie string: "c_user=...;xs=...;fr=...;datr=...|ACCESS_TOKEN"
	CookieString string `json:"cookie_string,omitempty"`

	// Cookie values (parsed or manual)
	Cookies map[string]string `json:"cookies"`
}

func New() *Config {
	return &Config{
		CommandPrefix: "!",
		Port:          "8080",
		Cookies:       make(map[string]string),
	}
}

// ParseCookieString parses a raw cookie string like
// "c_user=123;xs=abc;fr=def;datr=ghi" or
// "c_user=123;xs=abc;fr=def;datr=ghi|EAAAA..."
// into the Cookies map. The optional "|token" part is stored as "access_token".
func ParseCookieString(raw string) map[string]string {
	result := make(map[string]string)
	if raw == "" {
		return result
	}

	raw = strings.TrimSpace(raw)

	// Split off access token after "|"
	if idx := strings.LastIndex(raw, "|"); idx >= 0 {
		token := strings.TrimSpace(raw[idx+1:])
		if token != "" {
			result["access_token"] = token
		}
		raw = raw[:idx]
	}

	// Split cookie pairs by ";"
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if eqIdx := strings.Index(part, "="); eqIdx > 0 {
			key := strings.TrimSpace(part[:eqIdx])
			val := strings.TrimSpace(part[eqIdx+1:])
			if key != "" && val != "" {
				result[key] = val
			}
		}
	}

	return result
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

	// If cookie_string is provided, parse it and merge into cookies
	if cfg.CookieString != "" {
		parsed := ParseCookieString(cfg.CookieString)
		for k, v := range parsed {
			cfg.Cookies[k] = v
		}
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
	c.CookieString = newCfg.CookieString
	c.Cookies = newCfg.Cookies

	// If cookie_string is provided, parse and merge
	if c.CookieString != "" {
		if c.Cookies == nil {
			c.Cookies = make(map[string]string)
		}
		parsed := ParseCookieString(c.CookieString)
		for k, v := range parsed {
			c.Cookies[k] = v
		}
	}
}
