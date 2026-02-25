package main

import (
	"context"
	"os"
	"os/signal"
	"regexp"
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
	client       *messagix.Client
	cmds         *registry.Registry
	startTime    time.Time
	mediaService *mediaMod.Service
	selfID       int64
	seenMessages sync.Map
	msgSem       = make(chan struct{}, 100) // limit concurrent message handlers
	botReady     atomic.Bool
	connectTime  atomic.Int64 // unix milliseconds when bot connected
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

	// Load config
	cfgInit, err := config.Load("config.json")
	if err != nil {
		basic := zerolog.New(os.Stdout).With().Timestamp().Logger()
		basic.Fatal().Err(err).Msg("Failed to load config")
	}
	cfg = cfgInit

	logger = initLogger()

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
	if v, ok := modules[name]; ok {
		return v
	}
	// Default to true if not specified? Or false?
	// Given config.example.json has them explicitly, let's assume false if missing, or true if map is nil/empty (legacy)
	if len(modules) == 0 {
		return true
	}
	return false
}

func runBot(ctx context.Context) {
	botReady.Store(false)

	c := cookies.NewCookies()
	c.Platform = types.Facebook
	for k, v := range cfg.Cookies {
		if v != "" {
			c.Set(cookies.MetaCookieName(k), v)
		}
	}

	client = messagix.NewClient(c, logger, &messagix.Config{
		MayConnectToDGW: true,
	})

	client.SetEventHandler(handleEvent)

	user, _, err := client.LoadMessagesPage(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to load messages page")
		return
	}
	selfID = user.GetFBID()
	logger.Info().Int64("id", selfID).Msg("Logged in")

	err = client.Connect(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to connect")
	}

	<-ctx.Done()
	client.Disconnect()
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
		logger.Error().Err(e.Err).Msg("Socket error")
	}
}

func handleMessage(msg *WrappedMessage) {
	if msg == nil || msg.Text == "" {
		return
	}

	// Skip messages sent by the bot itself to prevent infinite loops
	if selfID != 0 && msg.SenderId == selfID {
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

	fbClient := facebook.NewClient(client, selfID)

	// Auto-detection logic (reusing MediaService)
	if mediaService != nil {
		urlMatch := urlRegex.FindString(msg.Text)
		if urlMatch != "" {
			urlMatch = strings.TrimRight(urlMatch, ".,;:!?\"'()[]{}><")
			processMediaAuto(context.Background(), fbClient, msg.ThreadKey, urlMatch)
			return
		}
	}

	if !strings.HasPrefix(msg.Text, cfg.CommandPrefix) {
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
		Ctx:       context.Background(),
		Sender:    fbClient,
		ThreadID:  msg.ThreadKey,
		SenderID:  msg.SenderId,
		Args:      args,
		RawText:   msg.Text,
		StartTime: startTime,
	}

	err := cmds.Execute(cmdName, ctx)
	if err != nil {
		logger.Error().Err(err).Msg("Command execution failed")
		fbClient.SendMessage(ctx.Ctx, ctx.ThreadID, "Lỗi: "+err.Error())
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
	attachments := make([]core.MediaAttachment, 0, len(medias))
	for range medias {
		r := <-results
		if r.err != nil {
			logger.Error().Err(r.err).Int("index", r.index).Msg("Failed to download media")
			continue
		}
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
