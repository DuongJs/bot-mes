package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type StorageConfig struct {
	MessageDBPath string `json:"message_db_path"`
}

// PerformanceConfig holds tuning knobs for throughput and resource usage.
type PerformanceConfig struct {
	// WorkerCount is the number of fixed message-handler goroutines.
	// Default: 20.
	WorkerCount int `json:"worker_count"`

	// JobQueueSize is the buffered-channel capacity for incoming messages.
	// Default: 500.
	JobQueueSize int `json:"job_queue_size"`

	// DBBatchSize is the max number of write operations grouped in one
	// SQLite transaction by the write-batcher.  Default: 100.
	DBBatchSize int `json:"db_batch_size"`

	// DBBatchFlushMs is the maximum time (ms) the batcher waits before
	// flushing a partial batch.  Default: 50.
	DBBatchFlushMs int `json:"db_batch_flush_ms"`

	// DBReadPoolSize is the number of reader connections for the SQLite
	// read pool (WAL mode).  Default: 4.
	DBReadPoolSize int `json:"db_read_pool_size"`

	// SendRatePerSecond is the global outgoing-message rate limit.
	// Default: 30.
	SendRatePerSecond int `json:"send_rate_per_second"`

	// SendBurst is the burst bucket size for the rate limiter.
	// Default: 10.
	SendBurst int `json:"send_burst"`

	// MessageHandlerTimeoutSeconds is the per-message context deadline.
	// Default: 30.
	MessageHandlerTimeoutSeconds int `json:"message_handler_timeout_seconds"`

	// MaxConcurrentDownloads is the system-wide limit on parallel media
	// downloads.  All users/groups share this pool.  Higher values give
	// better throughput when many users request media simultaneously,
	// but use more network bandwidth and temp disk space.
	// Default: 8.
	MaxConcurrentDownloads int `json:"max_concurrent_downloads"`
}

// Defaults returns a PerformanceConfig with sensible defaults.
func DefaultPerformanceConfig() PerformanceConfig {
	return PerformanceConfig{
		WorkerCount:                  20,
		JobQueueSize:                 500,
		DBBatchSize:                  100,
		DBBatchFlushMs:               50,
		DBReadPoolSize:               4,
		SendRatePerSecond:            30,
		SendBurst:                    10,
		MessageHandlerTimeoutSeconds: 30,
		MaxConcurrentDownloads:       16,
	}
}

// AutoLoginConfig holds credentials for automatic Facebook login
// when cookies are expired or missing.
type AutoLoginConfig struct {
	Enabled     bool   `json:"enabled"`
	UID         string `json:"uid"`
	Password    string `json:"password"`
	TwoFASecret string `json:"two_fa_secret"`
}

// TokensConfig stores the login tokens obtained from auto-login.
type TokensConfig struct {
	// LoginToken is the EAAAAU... token from the bloks login API (before session exchange)
	LoginToken string `json:"login_token"`
	// AccessToken from auth.getSessionForApp (after session exchange)
	AccessToken string `json:"access_token"`
}

type Config struct {
	mu sync.RWMutex

	CommandPrefix string `json:"command_prefix"`

	// Raw cookie string: "c_user=...;xs=...;fr=...;datr=...|ACCESS_TOKEN"
	CookieString string `json:"cookie_string,omitempty"`

	// Cookie values (parsed or manual)
	Cookies map[string]string `json:"cookies"`

	// Modules feature toggles
	Modules map[string]bool `json:"modules"`

	Storage StorageConfig `json:"storage"`

	// Performance tuning knobs.
	Performance PerformanceConfig `json:"performance"`

	// ForceRefreshIntervalSeconds is the interval in seconds between periodic
	// full reconnects. Set to 0 to disable. Default: 3600 (1 hour).
	ForceRefreshIntervalSeconds int `json:"force_refresh_interval_seconds"`

	// AutoLogin holds credentials for automatic login when cookies expire.
	AutoLogin AutoLoginConfig `json:"auto_login"`

	// Tokens stores login tokens obtained from auto-login.
	Tokens TokensConfig `json:"tokens"`
}

const DefaultForceRefreshInterval = 3600 // 1 hour

func New() *Config {
	return &Config{
		CommandPrefix:               "!",
		Cookies:                     make(map[string]string),
		Modules:                     make(map[string]bool),
		ForceRefreshIntervalSeconds: DefaultForceRefreshInterval,
		Storage: StorageConfig{
			MessageDBPath: "data/messages.sqlite",
		},
		Performance: DefaultPerformanceConfig(),
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
	if cfg.Storage.MessageDBPath == "" {
		cfg.Storage.MessageDBPath = "data/messages.sqlite"
	}

	// Apply performance defaults for zero-valued fields
	cfg.applyPerformanceDefaults()

	// If cookie_string is provided, parse it and merge into cookies
	cfg.mergeCookieString()

	return &cfg, nil
}

// mergeCookieString parses CookieString and merges results into Cookies map.
func (c *Config) mergeCookieString() {
	if c.CookieString == "" {
		return
	}
	if c.Cookies == nil {
		c.Cookies = make(map[string]string)
	}
	for k, v := range ParseCookieString(c.CookieString) {
		c.Cookies[k] = v
	}
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
	c.CookieString = newCfg.CookieString
	c.Cookies = newCfg.Cookies
	c.Modules = newCfg.Modules
	c.Storage = newCfg.Storage
	c.Performance = newCfg.Performance
	c.AutoLogin = newCfg.AutoLogin
	c.applyPerformanceDefaults()
	c.mergeCookieString()
}

// UpdateCookies updates the cookie string, map, and tokens, then saves to disk.
func (c *Config) UpdateCookies(cookieString string, cookies map[string]string, loginToken, accessToken, configPath string) error {
	c.mu.Lock()
	c.CookieString = cookieString
	for k, v := range cookies {
		c.Cookies[k] = v
	}
	c.Tokens = TokensConfig{
		LoginToken:  loginToken,
		AccessToken: accessToken,
	}
	c.mu.Unlock()
	return c.Save(configPath)
}

// applyPerformanceDefaults fills zero-valued performance fields with defaults
// and clamps excessively large values to safe upper bounds.
func (c *Config) applyPerformanceDefaults() {
	def := DefaultPerformanceConfig()
	p := &c.Performance
	if p.WorkerCount <= 0 {
		p.WorkerCount = def.WorkerCount
	}
	if p.JobQueueSize <= 0 {
		p.JobQueueSize = def.JobQueueSize
	}
	if p.DBBatchSize <= 0 {
		p.DBBatchSize = def.DBBatchSize
	}
	if p.DBBatchFlushMs <= 0 {
		p.DBBatchFlushMs = def.DBBatchFlushMs
	}
	if p.DBReadPoolSize <= 0 {
		p.DBReadPoolSize = def.DBReadPoolSize
	}
	if p.SendRatePerSecond <= 0 {
		p.SendRatePerSecond = def.SendRatePerSecond
	}
	if p.SendBurst <= 0 {
		p.SendBurst = def.SendBurst
	}
	if p.MessageHandlerTimeoutSeconds <= 0 {
		p.MessageHandlerTimeoutSeconds = def.MessageHandlerTimeoutSeconds
	}
	if p.MaxConcurrentDownloads <= 0 {
		p.MaxConcurrentDownloads = def.MaxConcurrentDownloads
	}

	// Clamp upper bounds to prevent unreasonable resource usage.
	clamp := func(val *int, max int) {
		if *val > max {
			*val = max
		}
	}
	clamp(&p.WorkerCount, 100)
	clamp(&p.JobQueueSize, 10000)
	clamp(&p.DBBatchSize, 1000)
	clamp(&p.DBReadPoolSize, 32)
	clamp(&p.MaxConcurrentDownloads, 64)

	// Ensure DBBatchSize does not exceed JobQueueSize.
	if p.DBBatchSize > p.JobQueueSize {
		p.DBBatchSize = p.JobQueueSize
	}
}

// UpdateModules updates only the Modules map.
func (c *Config) UpdateModules(modules map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Modules = modules
}

func ResolveMessageDBPath(configPath string, cfg *Config) (string, error) {
	if cfg == nil {
		cfg = New()
	}

	dbPath := cfg.Storage.MessageDBPath
	if dbPath == "" {
		dbPath = "data/messages.sqlite"
	}
	if filepath.IsAbs(dbPath) {
		return filepath.Clean(dbPath), nil
	}

	configExists := false
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			configExists = true
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}

	var baseDir string
	if configExists {
		absConfigPath, err := filepath.Abs(configPath)
		if err != nil {
			return "", err
		}
		baseDir = filepath.Dir(absConfigPath)
	} else {
		exePath, err := os.Executable()
		if err != nil {
			return "", err
		}
		baseDir = filepath.Dir(exePath)
	}

	return filepath.Clean(filepath.Join(baseDir, dbPath)), nil
}
