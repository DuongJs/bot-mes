package app

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-meta/pkg/messagix"

	"mybot/internal/config"
	"mybot/internal/media"
	"mybot/internal/messaging"
	"mybot/internal/metrics"
	mediaMod "mybot/internal/modules/media"
	"mybot/internal/registry"
	"mybot/internal/scripting"
	"mybot/internal/transport/facebook"
)

// Bot is the main application struct that encapsulates all state.
// It replaces the previous global variables with a single composable unit.
type Bot struct {
	Log        zerolog.Logger
	Cfg        *config.Config
	ConfigPath string

	messageAPI   *messaging.Service
	sender       *messaging.LegacySender
	mediaService *mediaMod.Service
	cmds         *registry.Registry
	workerPool   *messaging.WorkerPool
	startTime    time.Time

	clientMu sync.RWMutex
	client   *messagix.Client

	selfID       atomic.Int64
	botReady     atomic.Bool
	connectTime  atomic.Int64
	seenMessages sync.Map

	fullReconnectCh    chan struct{}
	stopPeriodicReconn atomic.Pointer[context.CancelFunc]
	metricStop         chan struct{}
}

// New creates a new Bot with the given config. It initializes storage,
// messaging, worker pool, and modules but does NOT connect yet.
func New(cfg *config.Config, configPath string, log zerolog.Logger) (*Bot, error) {
	b := &Bot{
		Log:             log,
		Cfg:             cfg,
		ConfigPath:      configPath,
		startTime:       time.Now(),
		fullReconnectCh: make(chan struct{}, 1),
	}

	// Purge leftover temp media files from previous runs.
	media.CleanupTempDir()

	if err := b.initStorage(); err != nil {
		return nil, err
	}
	b.initWorkerPool()
	b.registerModules()

	return b, nil
}

// Run starts the bot's connection loop and background tasks. It blocks
// until ctx is cancelled (e.g. via signal). This is the main entry point
// after New().
func (b *Bot) Run(ctx context.Context) {
	b.metricStop = make(chan struct{})
	b.startBackgroundTasks()

	// Connection loop: reconnects automatically on errors.
	go b.connectionLoop(ctx)

	// Block until context is cancelled (signal received).
	<-ctx.Done()

	b.Stop()
}

// Stop performs a graceful shutdown: stops workers, metrics, and closes the DB.
func (b *Bot) Stop() {
	if b.metricStop != nil {
		close(b.metricStop)
	}
	if b.workerPool != nil {
		b.workerPool.Stop()
	}

	b.clientMu.Lock()
	if b.client != nil {
		b.client.Disconnect()
		b.client = nil
	}
	b.clientMu.Unlock()

	if b.messageAPI != nil {
		if err := b.messageAPI.Close(); err != nil {
			b.Log.Error().Err(err).Msg("Failed to close message DB")
		}
	}

	b.Log.Info().Msg("Bot stopped")
}

// ── Initialization ─────────────────────────────────────────────────────────────

func (b *Bot) initStorage() error {
	dbPath, err := config.ResolveMessageDBPath(b.ConfigPath, b.Cfg)
	if err != nil {
		return err
	}
	store, err := messaging.OpenSQLiteStore(dbPath, b.Cfg.Performance.DBReadPoolSize)
	if err != nil {
		return err
	}
	batchedStore := messaging.NewBatchedStore(
		store, b.Log,
		b.Cfg.Performance.JobQueueSize,
		b.Cfg.Performance.DBBatchSize,
		b.Cfg.Performance.DBBatchFlushMs,
	)

	b.messageAPI = messaging.NewService(
		b.Log,
		batchedStore,
		func() int64 { return b.selfID.Load() },
		func() messaging.Transport {
			b.clientMu.RLock()
			c := b.client
			b.clientMu.RUnlock()
			if c == nil {
				return nil
			}
			return facebook.NewClient(c, b.selfID.Load(), b.messageAPI.Tracker())
		},
		func() *messagix.Client {
			b.clientMu.RLock()
			defer b.clientMu.RUnlock()
			return b.client
		},
		messaging.WithRateLimit(b.Cfg.Performance.SendRatePerSecond, b.Cfg.Performance.SendBurst),
	)
	b.sender = messaging.NewLegacySender(b.messageAPI)
	return nil
}

func (b *Bot) initWorkerPool() {
	b.workerPool = messaging.NewWorkerPool(
		b.Log,
		b.Cfg.Performance.WorkerCount,
		b.Cfg.Performance.JobQueueSize,
	)
}

func (b *Bot) registerModules() {
	b.cmds = registry.New()

	modulesDir := filepath.Join(filepath.Dir(b.ConfigPath), "modules")

	// Compiled module: media (requires download pool + internal APIs).
	if _, err := os.Stat(filepath.Join(modulesDir, "media")); err == nil {
		pool := media.NewDownloadPool(b.Cfg.Performance.MaxConcurrentDownloads)
		b.mediaService = mediaMod.NewService(pool)
		b.cmds.Register(mediaMod.NewCommand(b.mediaService, b.Log))
		if token := b.Cfg.Tokens.AccessToken; token != "" {
			media.SetFacebookToken(token)
			b.Log.Info().Msg("Facebook GraphQL token set from config")
		}
		b.Log.Info().Int("max_concurrent", pool.Capacity()).Msg("Media download pool initialized")
	}

	// Script modules: auto-loaded from modules/ subdirectories via Yaegi.
	compiledModules := map[string]bool{"media": true}
	scriptCmds, scriptErrs := scripting.LoadModules(modulesDir, compiledModules)
	for _, err := range scriptErrs {
		b.Log.Error().Err(err).Msg("Failed to load script module")
	}
	lister := func() map[string]string { return b.cmds.List() }
	for _, cmd := range scriptCmds {
		cmd.SetCommandLister(lister)
		b.cmds.Register(cmd)
		b.Log.Info().Str("name", cmd.Name()).Msg("Loaded script module")
	}
}

// ── Background Tasks ───────────────────────────────────────────────────────────

func (b *Bot) startBackgroundTasks() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				b.cmds.CleanCooldowns()
				b.seenMessages.Clear()
				b.Log.Debug().Msg("Periodic cleanup: cooldowns + seen messages")
			case <-b.metricStop:
				return
			}
		}
	}()
	go metrics.StartPeriodicLog(b.Log, 60*time.Second, b.metricStop)
}

// triggerReconnect signals the connection loop to reconnect.
func (b *Bot) triggerReconnect() {
	select {
	case b.fullReconnectCh <- struct{}{}:
	default:
	}
}
