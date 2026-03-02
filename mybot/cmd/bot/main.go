package main

import (
	"context"
	"os"
	"os/signal"
	"regexp"
	"sort"
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
	"mybot/internal/media"
	"mybot/internal/messaging"
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
	msgSem       = make(chan struct{}, 100) // limit concurrent message handlers
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
	const configPath = "config.json"

	startTime = time.Now()

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
	store, err := messaging.OpenSQLiteStore(dbPath)
	if err != nil {
		logger.Fatal().Err(err).Str("path", dbPath).Msg("Failed to open message DB")
	}
	messageAPI = messaging.NewService(
		logger,
		store,
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
	)
	legacySender = messaging.NewLegacySender(messageAPI)
	defer func() {
		if err := messageAPI.Close(); err != nil {
			logger.Error().Err(err).Msg("Failed to close message DB")
		}
	}()

	cmds = registry.New()

	// Register Modules
	if enabled(cfg.Modules, "ping") {
		cmds.Register(&ping.Command{})
	}

	if enabled(cfg.Modules, "media") {
		mediaService = mediaMod.NewService()
		cmds.Register(mediaMod.NewCommand(mediaService))
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
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cmds.CleanCooldowns()
			seenMessages.Clear()
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	go runBot(ctx)

	// Wait for termination signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	cancel()
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
		logger.Error().Err(err).Msg("Failed to load messages page, retrying in 30s...")
		select {
		case <-time.After(30 * time.Second):
		case <-ctx.Done():
		}
		fullReconnect()
		return
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
				msgSem <- struct{}{}
				go func() {
					defer func() { <-msgSem }()
					handleMessage(msg)
				}()
			}
			for _, m := range e.Table.LSInsertMessage {
				msg := &WrappedMessage{
					ThreadKey:   m.ThreadKey,
					Text:        m.Text,
					SenderId:    m.SenderId,
					MessageId:   m.MessageId,
					TimestampMs: m.TimestampMs,
				}
				msgSem <- struct{}{}
				go func() {
					defer func() { <-msgSem }()
					handleMessage(msg)
				}()
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
				processMediaAuto(context.Background(), legacySender, msg.ThreadKey, urlMatch)
			}
		}
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

	ctx := &core.CommandContext{
		Ctx:               context.Background(),
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

	// Download all items in parallel
	type downloadResult struct {
		index    int
		data     []byte
		mime     string
		filename string
		err      error
	}

	results := make(chan downloadResult, len(medias))
	for i, m := range medias {
		go func(idx int, item media.MediaItem) {
			data, mime, err := mediaService.Download(ctx, item.URL)
			if err != nil {
				results <- downloadResult{index: idx, err: err}
				return
			}
			results <- downloadResult{
				index:    idx,
				data:     data,
				mime:     mime,
				filename: media.FilenameFromMIME(mime),
			}
		}(i, m)
	}

	// Collect all downloads
	downloaded := make([]downloadResult, 0, len(medias))
	for range medias {
		r := <-results
		if r.err != nil {
			logger.Error().Err(r.err).Int("index", r.index).Msg("Failed to download media")
			continue
		}
		downloaded = append(downloaded, r)
	}

	// Sort by original index to preserve media order
	sort.Slice(downloaded, func(i, j int) bool {
		return downloaded[i].index < downloaded[j].index
	})

	attachments := make([]core.MediaAttachment, 0, len(downloaded))
	for _, r := range downloaded {
		attachments = append(attachments, core.MediaAttachment{
			Data:     r.data,
			Filename: r.filename,
			MimeType: r.mime,
		})
	}

	if len(attachments) == 0 {
		return
	}

	// Send all as one message
	if err := sender.SendMultiMedia(ctx, threadID, attachments); err != nil {
		logger.Error().Err(err).Msg("Failed to send media")
	}
}
