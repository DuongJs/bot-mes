package main

import (
	"context"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-meta/pkg/messagix"
	"go.mau.fi/mautrix-meta/pkg/messagix/cookies"
	"go.mau.fi/mautrix-meta/pkg/messagix/types"

	"mybot/internal/config"
	"mybot/internal/core"
	"mybot/internal/dashboard"
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
	w := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}
	return zerolog.New(w).With().Timestamp().Logger()
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
)

type WrappedMessage struct {
	ThreadKey int64
	Text      string
	SenderId  int64
	MessageId string
}

func main() {
	logger = initLogger()
	startTime = time.Now()

	var err error
	cfg, err = config.Load("config.json")
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to load config")
	}

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

	// Setup restart channel
	restartChan := make(chan struct{})

	// Start dashboard
	dash := dashboard.New(cfg, func() {
		logger.Info().Msg("Restart triggered from dashboard")
		restartChan <- struct{}{}
	})
	dash.Commands = cmds
	dash.Start(cfg.Port)

	for {
		ctx, cancel := context.WithCancel(context.Background())
		go runBot(ctx)

		// Wait for signal or restart request
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

		select {
		case <-sig:
			cancel()
			return
		case <-restartChan:
			cancel()
			logger.Info().Msg("Restarting bot...")
			// Reload config
			cfg, err = config.Load("config.json")
			if err != nil {
				logger.Error().Err(err).Msg("Failed to reload config")
			}
			// Update modules if needed (re-registering might require clearing old ones or creating new registry)
			// For simplicity, we just reload config values, but adding/removing modules requires logic.
			// Currently, we just restart the bot loop, but registry is initialized outside.
			// Ideally we should re-init registry here too.
		}
	}
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
	case *messagix.Event_PublishResponse:
		if e.Table != nil {
			logger.Info().
				Int("upsert_message_count", len(e.Table.LSUpsertMessage)).
				Int("insert_message_count", len(e.Table.LSInsertMessage)).
				Msg("Received table update")

			for _, m := range e.Table.LSUpsertMessage {
				handleMessage(&WrappedMessage{
					ThreadKey: m.ThreadKey,
					Text:      m.Text,
					SenderId:  m.SenderId,
					MessageId: m.MessageId,
				})
			}
			for _, m := range e.Table.LSInsertMessage {
				handleMessage(&WrappedMessage{
					ThreadKey: m.ThreadKey,
					Text:      m.Text,
					SenderId:  m.SenderId,
					MessageId: m.MessageId,
				})
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

	// Deduplicate: skip messages we've already processed
	if _, loaded := seenMessages.LoadOrStore(msg.MessageId, struct{}{}); loaded {
		return
	}

	fbClient := facebook.NewClient(client, selfID)

	// Auto-detection logic (reusing MediaService)
	if mediaService != nil {
		urlMatch := urlRegex.FindString(msg.Text)
		if urlMatch != "" {
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
		// Silent fail on auto-detection usually, or log
		return
	}
	if len(medias) == 0 {
		return
	}

	logger.Info().Str("url", url).Int("count", len(medias)).Msg("Auto-detected media")

	var wg sync.WaitGroup
	for i, m := range medias {
		wg.Add(1)
		go func(idx int, item media.MediaItem) {
			defer wg.Done()

			data, mime, err := mediaService.Download(ctx, item.URL)
			if err != nil {
				logger.Error().Err(err).Msg("Failed to download media")
				return
			}

			if err := sender.SendMedia(ctx, threadID, data, media.FilenameFromMIME(mime), mime); err != nil {
				logger.Error().Err(err).Msg("Failed to send media")
			}
		}(i, m)
	}
	wg.Wait()
}
