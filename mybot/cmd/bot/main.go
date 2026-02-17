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
	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
	"go.mau.fi/mautrix-meta/pkg/messagix/types"

	"mybot/internal/commands"
	"mybot/internal/config"
	"mybot/internal/dashboard"
	"mybot/internal/media"
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
	logger    zerolog.Logger
	cfg       *config.Config
	client    *messagix.Client
	cmds      *commands.Registry
	startTime time.Time
)

func main() {
	logger = initLogger()
	startTime = time.Now()

	var err error
	cfg, err = config.Load("config.json")
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to load config")
	}

	cmds = commands.NewRegistry()
	cmds.Register("ping", &commands.PingCommand{})
	cmds.Register("help", &commands.HelpCommand{Registry: cmds})
	cmds.Register("media", &commands.MediaCommand{})
	cmds.Register("uptime", &commands.UptimeCommand{})
	cmds.Register("about", &commands.AboutCommand{})
	cmds.Register("status", &commands.StatusCommand{})
	cmds.Register("id", &commands.IDCommand{})

	// Periodically clean expired cooldowns to prevent memory buildup
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cmds.CleanCooldowns()
		}
	}()

	// Setup restart channel
	restartChan := make(chan struct{})

	// Start dashboard
	dash := dashboard.New(cfg, func() {
		logger.Info().Msg("Restart triggered from dashboard")
		restartChan <- struct{}{}
	})
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
		}
	}
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
	logger.Info().Int64("id", user.GetFBID()).Msg("Logged in")

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
				handleMessage(&commands.WrappedMessage{
					ThreadKey: m.ThreadKey,
					Text:      m.Text,
					SenderId:  m.SenderId,
					MessageId: m.MessageId,
				})
			}
			for _, m := range e.Table.LSInsertMessage {
				handleMessage(&commands.WrappedMessage{
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

func handleMessage(msg *commands.WrappedMessage) {
	if msg == nil || msg.Text == "" {
		return
	}

	urlMatch := urlRegex.FindString(msg.Text)

	if urlMatch != "" {
		// Try to download media
		medias, err := media.GetMedia(context.Background(), urlMatch)
		if err == nil && len(medias) > 0 {
			logger.Info().Str("url", urlMatch).Int("count", len(medias)).Msg("Auto-detected media from URL")

			type uploadResult struct {
				fbID int64
			}
			var wg sync.WaitGroup
			results := make(chan uploadResult, len(medias))

			for _, m := range medias {
				wg.Add(1)
				go func(item media.MediaItem) {
					defer wg.Done()
					data, mime, err := media.DownloadMedia(context.Background(), item.URL)
					if err != nil {
						logger.Error().Err(err).Msg("Failed to download media")
						return
					}

					uploadResp, err := client.SendMercuryUploadRequest(context.Background(), msg.ThreadKey, &messagix.MercuryUploadMedia{
						Filename:  "media",
						MimeType:  mime,
						MediaData: data,
					})
					if err != nil {
						logger.Error().Err(err).Msg("Failed to upload media")
						return
					}

					var realFBID int64
					if uploadResp.Payload.RealMetadata != nil {
						realFBID = uploadResp.Payload.RealMetadata.GetFbId()
					}
					if realFBID != 0 {
						results <- uploadResult{fbID: realFBID}
					} else {
						logger.Error().Msg("Failed to get media ID")
					}
				}(m)
			}

			go func() {
				wg.Wait()
				close(results)
			}()

			for r := range results {
				task := &socket.SendMessageTask{
					ThreadId:        msg.ThreadKey,
					AttachmentFBIds: []int64{r.fbID},
					Source:          table.MESSENGER_INBOX_IN_THREAD,
					SendType:        table.MEDIA,
					SyncGroup:       1,
					Otid:            methods.GenerateEpochID(),
				}
				client.ExecuteTask(context.Background(), task)
			}
			return // Stop processing if media was handled
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
	err := cmds.Execute(cmdName, &commands.Context{
		Ctx:       context.Background(),
		Client:    client,
		Message:   msg,
		Args:      args,
		StartTime: startTime,
	})
	if err != nil {
		logger.Error().Err(err).Msg("Command execution failed")
	}
}
