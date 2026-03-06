package main

import (
	"context"
	"os"
	"os/signal"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-meta/pkg/messagix"
	"go.mau.fi/mautrix-meta/pkg/messagix/cookies"
	"go.mau.fi/mautrix-meta/pkg/messagix/types"

	"mybot/internal/config"
	"mybot/internal/core"
	"mybot/internal/fblogin"
	"mybot/internal/media"
	"mybot/internal/messaging"
	"mybot/internal/metrics"
	"mybot/internal/modules/coinflip"
	"mybot/internal/modules/help"
	"mybot/internal/modules/info"
	mediaMod "mybot/internal/modules/media"
	"mybot/internal/modules/ping"
	"mybot/internal/modules/roll"
	"mybot/internal/modules/say"
	"mybot/internal/modules/uptime"
	"mybot/internal/registry"
	"mybot/internal/transport/facebook"
)

func initLogger() zerolog.Logger {
	if os.Getenv("LOG_FORMAT") == "json" {
		return zerolog.New(os.Stdout).With().Timestamp().Logger()
	}
	consoleW := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}
	return zerolog.New(consoleW).With().Timestamp().Logger()
}

var urlRegex = regexp.MustCompile(`https?://\S+`)

var (
	logger       zerolog.Logger
	cfg          *config.Config
	clientMu     sync.RWMutex
	client       *messagix.Client
	cmds         *registry.Registry
	messageAPI   *messaging.Service
	legacySender *messaging.LegacySender
	startTime    time.Time
	mediaService *mediaMod.Service
	selfID       atomic.Int64
	seenMessages sync.Map
	workerPool   *messaging.WorkerPool
	botReady     atomic.Bool
	connectTime  atomic.Int64 // unix milliseconds when bot connected

	// fullReconnectCh signals the bot loop to perform a full reconnect
	// (disconnect, reload messages page, reconnect).
	fullReconnectCh     = make(chan struct{}, 1)
	stopPeriodicReconn  atomic.Pointer[context.CancelFunc]
)

type WrappedMessage struct {
	ThreadKey   int64
	Text        string
	SenderId    int64
	MessageId   string
	TimestampMs int64
}

func main() {
	startTime = time.Now()

	// Purge leftover temp media files from previous runs.
	media.CleanupTempDir()

	// Load config
	cfgInit, err := config.Load(configPath)
	if err != nil {
		basic := zerolog.New(os.Stdout).With().Timestamp().Logger()
		basic.Fatal().Err(err).Msg("Failed to load config")
	}
	cfg = cfgInit

	logger = initLogger()

	dbPath, err := config.ResolveMessageDBPath(configPath, cfg)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to resolve message DB path")
	}
	store, err := messaging.OpenSQLiteStore(dbPath, cfg.Performance.DBReadPoolSize)
	if err != nil {
		logger.Fatal().Err(err).Str("path", dbPath).Msg("Failed to open message DB")
	}
	batchedStore := messaging.NewBatchedStore(
		store, logger,
		cfg.Performance.JobQueueSize,
		cfg.Performance.DBBatchSize,
		cfg.Performance.DBBatchFlushMs,
	)
	messageAPI = messaging.NewService(
		logger,
		batchedStore,
		func() int64 { return selfID.Load() },
		func() messaging.Transport {
			clientMu.RLock()
			c := client
			clientMu.RUnlock()
			if c == nil {
				return nil
			}
			return facebook.NewClient(c, selfID.Load(), messageAPI.Tracker())
		},
		func() *messagix.Client {
			clientMu.RLock()
			defer clientMu.RUnlock()
			return client
		},
		messaging.WithRateLimit(cfg.Performance.SendRatePerSecond, cfg.Performance.SendBurst),
	)
	legacySender = messaging.NewLegacySender(messageAPI)
	defer func() {
		if err := messageAPI.Close(); err != nil {
			logger.Error().Err(err).Msg("Failed to close message DB")
		}
	}()

	// Start worker pool
	workerPool = messaging.NewWorkerPool(
		logger,
		cfg.Performance.WorkerCount,
		cfg.Performance.JobQueueSize,
	)
	defer workerPool.Stop()

	cmds = registry.New()

	// Register Modules
	if enabled(cfg.Modules, "ping") {
		cmds.Register(&ping.Command{})
	}

	if enabled(cfg.Modules, "media") {
		downloadPool := media.NewDownloadPool(cfg.Performance.MaxConcurrentDownloads)
		mediaService = mediaMod.NewService(downloadPool)
		cmds.Register(mediaMod.NewCommand(mediaService))
		logger.Info().Int("max_concurrent", downloadPool.Capacity()).Msg("Media download pool initialized")
	}

	if enabled(cfg.Modules, "help") {
		cmds.Register(help.NewCommand(cmds))
	}

	if enabled(cfg.Modules, "uptime") {
		cmds.Register(&uptime.Command{})
	}

	if enabled(cfg.Modules, "info") {
		cmds.Register(&info.AboutCommand{})
		cmds.Register(&info.IDCommand{})
		cmds.Register(&info.StatusCommand{})
	}

	if enabled(cfg.Modules, "say") {
		cmds.Register(&say.Command{})
	}

	if enabled(cfg.Modules, "coinflip") {
		cmds.Register(&coinflip.Command{})
	}

	if enabled(cfg.Modules, "roll") {
		cmds.Register(&roll.Command{})
	}

	// Periodically clean expired cooldowns and seen messages
	metricStop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cmds.CleanCooldowns()
			seenMessages.Clear()
		}
	}()

	// Start periodic metrics logging
	go metrics.StartPeriodicLog(logger, 60*time.Second, metricStop)

	ctx, cancel := context.WithCancel(context.Background())
	go runBot(ctx)

	// Wait for termination signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	cancel()
	close(metricStop)
	logger.Info().Msg("Bot stopped")
}

func enabled(modules map[string]bool, name string) bool {
	if len(modules) == 0 {
		// No modules configured at all → enable everything (legacy/default).
		return true
	}
	// Explicitly configured → return the value, default false if absent.
	return modules[name]
}

// fullReconnect tears down the current connection and starts fresh.
func fullReconnect() {
	select {
	case fullReconnectCh <- struct{}{}:
	default:
		// already pending
	}
}

func runBot(ctx context.Context) {
	for {
		botReady.Store(false)
		runBotOnce(ctx)

		if ctx.Err() != nil {
			return
		}

		// Wait for a full reconnect signal or context cancellation
		select {
		case <-fullReconnectCh:
			logger.Info().Msg("Full reconnect requested, restarting bot session...")
		case <-ctx.Done():
			return
		}
	}
}

func runBotOnce(ctx context.Context) {
	// Auto-login: if enabled and cookies look empty, login first
	if cfg.AutoLogin.Enabled && cfg.AutoLogin.UID != "" && cfg.AutoLogin.Password != "" {
		if !hasCookies(cfg.Cookies) {
			logger.Info().Msg("No valid cookies found, attempting auto-login...")
			if err := doAutoLogin(); err != nil {
				logger.Error().Err(err).Msg("Auto-login failed")
			} else {
				logger.Info().Msg("Auto-login succeeded, cookies updated")
			}
		}
	}

	c := cookies.NewCookies()
	c.Platform = types.Facebook
	for k, v := range cfg.Cookies {
		if v != "" {
			c.Set(cookies.MetaCookieName(k), v)
		}
	}

	newClient := messagix.NewClient(c, logger, &messagix.Config{
		MayConnectToDGW: true,
	})
	clientMu.Lock()
	client = newClient
	clientMu.Unlock()

	newClient.SetEventHandler(handleEvent)

	user, initialTable, err := newClient.LoadMessagesPage(ctx)
	if err != nil {
		// If auto-login is enabled, try to re-login and retry once
		if cfg.AutoLogin.Enabled && cfg.AutoLogin.UID != "" && cfg.AutoLogin.Password != "" {
			logger.Warn().Err(err).Msg("LoadMessagesPage failed, attempting auto-login...")
			if loginErr := doAutoLogin(); loginErr != nil {
				logger.Error().Err(loginErr).Msg("Auto-login failed")
			} else {
				logger.Info().Msg("Auto-login succeeded, retrying connection...")
				// Rebuild cookies and client with fresh cookies
				c2 := cookies.NewCookies()
				c2.Platform = types.Facebook
				for k, v := range cfg.Cookies {
					if v != "" {
						c2.Set(cookies.MetaCookieName(k), v)
					}
				}
				newClient = messagix.NewClient(c2, logger, &messagix.Config{
					MayConnectToDGW: true,
				})
				clientMu.Lock()
				client = newClient
				clientMu.Unlock()
				newClient.SetEventHandler(handleEvent)

				user, initialTable, err = newClient.LoadMessagesPage(ctx)
			}
		}
		if err != nil {
			logger.Error().Err(err).Msg("Failed to load messages page, retrying in 30s...")
			select {
			case <-time.After(30 * time.Second):
			case <-ctx.Done():
			}
			fullReconnect()
			return
		}
	}
	selfID.Store(user.GetFBID())
	logger.Info().Int64("id", selfID.Load()).Msg("Logged in")
	if err := messageAPI.ObserveTable(ctx, initialTable, messaging.MetadataOnly); err != nil {
		logger.Warn().Err(err).Msg("Failed to seed startup metadata")
	}

	err = newClient.Connect(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to connect")
		return
	}

	// Start periodic reconnect goroutine
	go periodicReconnect()

	// Block until full reconnect or shutdown
	select {
	case <-fullReconnectCh:
		logger.Info().Msg("Full reconnect triggered, tearing down current session...")
	case <-ctx.Done():
	}

	// Stop periodic reconnect
	if cancel := stopPeriodicReconn.Load(); cancel != nil {
		(*cancel)()
	}

	clientMu.Lock()
	if client != nil {
		client.Disconnect()
		client = nil
	}
	clientMu.Unlock()

	// Re-push the signal so runBot outer loop picks it up
	if ctx.Err() == nil {
		fullReconnect()
	}
}

// periodicReconnect triggers a full reconnect on a timer.
func periodicReconnect() {
	intervalSec := cfg.ForceRefreshIntervalSeconds
	if intervalSec <= 0 {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if oldCancel := stopPeriodicReconn.Swap(&cancel); oldCancel != nil {
		(*oldCancel)()
	}

	interval := time.Duration(intervalSec) * time.Second
	timer := time.NewTimer(interval)
	defer timer.Stop()

	logger.Info().Stringer("interval", interval).Msg("Periodic reconnect loop started")

	for {
		select {
		case <-timer.C:
			logger.Info().Msg("Periodic reconnect timer fired")
			fullReconnect()
			return
		case <-ctx.Done():
			return
		}
	}
}

func handleEvent(ctx context.Context, evt any) {
	switch e := evt.(type) {
	case *messagix.Event_Ready:
		connectTime.Store(time.Now().UnixMilli())
		botReady.Store(true)
		logger.Info().Msg("Bot is ready to process messages")
	case *messagix.Event_Reconnected:
		connectTime.Store(time.Now().UnixMilli())
		botReady.Store(true)
		logger.Info().Msg("Bot reconnected, ready to process messages")
	case *messagix.Event_PublishResponse:
		if e.Table != nil {
			if err := messageAPI.ObserveTable(ctx, e.Table, messaging.FullEvents); err != nil {
				logger.Warn().Err(err).Msg("Failed to project table update")
			}
		}
		if !botReady.Load() {
			return
		}
		if e.Table != nil {
			logger.Info().
				Int("upsert_message_count", len(e.Table.LSUpsertMessage)).
				Int("insert_message_count", len(e.Table.LSInsertMessage)).
				Msg("Received table update")

			for _, m := range e.Table.LSUpsertMessage {
				msg := &WrappedMessage{
					ThreadKey:   m.ThreadKey,
					Text:        m.Text,
					SenderId:    m.SenderId,
					MessageId:   m.MessageId,
					TimestampMs: m.TimestampMs,
				}
				metrics.Global.MessagesReceived.Add(1)
				workerPool.Submit(func() {
					handleMessage(msg)
				})
			}
			for _, m := range e.Table.LSInsertMessage {
				msg := &WrappedMessage{
					ThreadKey:   m.ThreadKey,
					Text:        m.Text,
					SenderId:    m.SenderId,
					MessageId:   m.MessageId,
					TimestampMs: m.TimestampMs,
				}
				metrics.Global.MessagesReceived.Add(1)
				workerPool.Submit(func() {
					handleMessage(msg)
				})
			}
		}
	case *messagix.Event_SocketError:
		logger.Error().Err(e.Err).Int("attempts", e.ConnectionAttempts).Msg("Socket error")
		// After many failed socket-level reconnects, do a full reconnect
		if e.ConnectionAttempts >= 10 {
			logger.Warn().Msg("Too many failed reconnect attempts, triggering full reconnect")
			fullReconnect()
		}
	case *messagix.Event_PermanentError:
		logger.Error().Err(e.Err).Msg("Permanent connection error, triggering full reconnect in 30s")
		botReady.Store(false)
		go func() {
			time.Sleep(30 * time.Second)
			fullReconnect()
		}()
	}
}

func handleMessage(msg *WrappedMessage) {
	if msg == nil || msg.Text == "" {
		return
	}

	// Skip messages sent by the bot itself to prevent infinite loops
	if sid := selfID.Load(); sid != 0 && msg.SenderId == sid {
		return
	}

	// Skip messages older than when the bot connected
	if msg.TimestampMs > 0 && msg.TimestampMs < connectTime.Load() {
		return
	}

	// Deduplicate: skip messages we've already processed
	if _, loaded := seenMessages.LoadOrStore(msg.MessageId, struct{}{}); loaded {
		return
	}

	// Check if message is a command first (commands take priority over auto-detect)
	if strings.HasPrefix(msg.Text, cfg.CommandPrefix) {
		// Fall through to command processing below
	} else {
		// Auto-detection logic for non-command messages
		if mediaService != nil {
			urlMatch := urlRegex.FindString(msg.Text)
			if urlMatch != "" {
				urlMatch = strings.TrimRight(urlMatch, ".,;:!?\"'()[]{}><")
				timeout := time.Duration(cfg.Performance.MessageHandlerTimeoutSeconds) * time.Second
				autoCtx, autoCancel := context.WithTimeout(context.Background(), timeout)
				defer autoCancel()
				processMediaAuto(autoCtx, legacySender, msg.ThreadKey, urlMatch)
			}
		}
		metrics.Global.MessagesProcessed.Add(1)
		return
	}

	fullCmd := strings.TrimPrefix(msg.Text, cfg.CommandPrefix)
	parts := strings.Fields(fullCmd)
	if len(parts) == 0 {
		return
	}

	cmdName := parts[0]
	args := parts[1:]

	logger.Info().Str("cmd", cmdName).Msg("Processing command")

	timeout := time.Duration(cfg.Performance.MessageHandlerTimeoutSeconds) * time.Second
	cmdCtx, cmdCancel := context.WithTimeout(context.Background(), timeout)
	defer cmdCancel()

	ctx := &core.CommandContext{
		Ctx:               cmdCtx,
		Sender:            legacySender,
		Messages:          messageAPI,
		Conversation:      messageAPI,
		ThreadID:          msg.ThreadKey,
		SenderID:          msg.SenderId,
		IncomingMessageID: msg.MessageId,
		Args:              args,
		RawText:           msg.Text,
		StartTime:         startTime,
	}

	err := cmds.Execute(cmdName, ctx)
	if err != nil {
		logger.Error().Err(err).Msg("Command execution failed")
		legacySender.SendMessage(ctx.Ctx, ctx.ThreadID, "Lỗi: "+err.Error())
	}
	metrics.Global.CommandsExecuted.Add(1)
	metrics.Global.MessagesProcessed.Add(1)
}

func processMediaAuto(ctx context.Context, sender core.MessageSender, threadID int64, url string) {
	medias, err := mediaService.GetMediaItems(ctx, url)
	if err != nil {
		logger.Warn().Err(err).Str("url", url).Msg("Auto-detect media failed")
		return
	}
	if len(medias) == 0 {
		return
	}

	logger.Info().Str("url", url).Int("count", len(medias)).Msg("Auto-detected media")

	// Download via global pool — respects system-wide concurrency limit.
	results := mediaService.DownloadBatch(ctx, medias)

	// Ensure all temp files are cleaned up when done.
	defer func() {
		for i := range results {
			if results[i].File != nil {
				results[i].File.Cleanup()
			}
		}
		debug.FreeOSMemory()
	}()

	var attachments []core.MediaAttachment
	for _, r := range results {
		if r.Err != nil {
			logger.Error().Err(r.Err).Int("index", r.Index).Msg("Failed to download media")
			continue
		}
		attachments = append(attachments, core.MediaAttachment{
			FilePath: r.File.Path,
			FileSize: r.File.Size,
			Filename: r.File.Filename,
			MimeType: r.File.MimeType,
		})
	}

	if len(attachments) == 0 {
		return
	}

	// Send — upload reads one file at a time from disk.
	if err := sender.SendMultiMedia(ctx, threadID, attachments); err != nil {
		logger.Error().Err(err).Msg("Failed to send media")
	}
}

// ── Auto-login helpers ─────────────────────────────────────────────────────────

const configPath = "config.json"

// hasCookies returns true if the cookies map has non-empty c_user and xs values.
func hasCookies(cookies map[string]string) bool {
	return cookies["c_user"] != "" && cookies["xs"] != ""
}

// doAutoLogin performs a Facebook login using credentials from config,
// then updates the config with fresh cookies and both tokens, and saves to disk.
func doAutoLogin() error {
	result, err := fblogin.Login(cfg.AutoLogin.UID, cfg.AutoLogin.Password, cfg.AutoLogin.TwoFASecret)
	if err != nil {
		return err
	}

	logger.Info().
		Str("login_token", result.LoginToken[:20]+"...").
		Str("access_token", result.AccessToken[:20]+"...").
		Msg("Auto-login obtained tokens")

	return cfg.UpdateCookies(result.CookieString, result.Cookies, result.LoginToken, result.AccessToken, configPath)
}
