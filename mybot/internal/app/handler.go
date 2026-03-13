package app

import (
	"context"
	"net/url"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"mybot/internal/core"
	"mybot/internal/metrics"
)

var urlRegex = regexp.MustCompile(`https?://\S+`)

// WrappedMessage holds the fields we care about from LSUpsertMessage / LSInsertMessage.
type WrappedMessage struct {
	ThreadKey    int64
	Text         string
	SenderId     int64
	MessageId    string
	TimestampMs  int64
	TextHasLinks bool
	XMAUrl       string
}

// handleMessage processes a single incoming message: either as a command or
// as auto-detect media.
func (b *Bot) handleMessage(msg *WrappedMessage) {
	if msg == nil {
		return
	}

	// Determine effective text: use Text, or fall back to XMA URL for shared links.
	effectiveText := msg.Text
	if effectiveText == "" && msg.XMAUrl != "" {
		b.Log.Debug().
			Int64("thread", msg.ThreadKey).
			Int64("sender", msg.SenderId).
			Str("msg_id", msg.MessageId).
			Str("xma_url", msg.XMAUrl).
			Msg("[DEBUG] Empty text but has XMA URL, using XMA URL")
		effectiveText = msg.XMAUrl
	}

	if effectiveText == "" {
		return
	}

	// Skip self messages.
	if sid := b.selfID.Load(); sid != 0 && msg.SenderId == sid {
		return
	}

	// Skip messages older than when the bot connected.
	if msg.TimestampMs > 0 && msg.TimestampMs < b.connectTime.Load() {
		return
	}

	// Deduplicate.
	if _, loaded := b.seenMessages.LoadOrStore(msg.MessageId, struct{}{}); loaded {
		return
	}

	b.Log.Debug().
		Int64("thread", msg.ThreadKey).
		Int64("sender", msg.SenderId).
		Str("text", msg.Text).
		Str("effective_text", effectiveText).
		Bool("has_links", msg.TextHasLinks).
		Str("xma_url", msg.XMAUrl).
		Str("msg_id", msg.MessageId).
		Msg("[DEBUG] Processing message")

	// Command takes priority over auto-detect.
	if strings.HasPrefix(effectiveText, b.Cfg.CommandPrefix) {
		msg.Text = effectiveText
		b.dispatchCommand(msg)
		return
	}

	// Auto-detect media URLs.
	b.autoDetectMedia(msg, effectiveText)
	metrics.Global.MessagesProcessed.Add(1)
}

// dispatchCommand parses and executes a bot command.
func (b *Bot) dispatchCommand(msg *WrappedMessage) {
	fullCmd := strings.TrimPrefix(msg.Text, b.Cfg.CommandPrefix)
	parts := strings.Fields(fullCmd)
	if len(parts) == 0 {
		return
	}

	cmdName := parts[0]
	args := parts[1:]

	b.Log.Info().Str("cmd", cmdName).Msg("Processing command")

	// Use longer timeout for media-related commands.
	timeout := time.Duration(b.Cfg.Performance.MessageHandlerTimeoutSeconds) * time.Second
	if cmdName == "media" {
		timeout = time.Duration(b.Cfg.Performance.MediaCommandTimeoutSeconds) * time.Second
	}
	cmdCtx, cmdCancel := context.WithTimeout(context.Background(), timeout)
	defer cmdCancel()

	ctx := &core.CommandContext{
		Ctx:               cmdCtx,
		Sender:            b.sender,
		Messages:          b.messageAPI,
		Conversation:      b.messageAPI,
		ThreadID:          msg.ThreadKey,
		SenderID:          msg.SenderId,
		IncomingMessageID: msg.MessageId,
		Args:              args,
		RawText:           msg.Text,
		StartTime:         b.startTime,
	}

	if err := b.cmds.Execute(cmdName, ctx); err != nil {
		b.Log.Error().Err(err).Msg("Command execution failed")
		b.sender.SendMessage(ctx.Ctx, ctx.ThreadID, "Lỗi: "+err.Error())
	}
	metrics.Global.CommandsExecuted.Add(1)
	metrics.Global.MessagesProcessed.Add(1)
}

// autoDetectMedia tries to extract a URL from the message and download media.
func (b *Bot) autoDetectMedia(msg *WrappedMessage, effectiveText string) {
	if b.mediaService == nil {
		return
	}

	urlMatch := urlRegex.FindString(effectiveText)
	if urlMatch != "" {
		urlMatch = strings.TrimRight(urlMatch, ".,;:!?\"'()[]{}><")
	}

	// Fall back to XMA URL if regex didn't match.
	if urlMatch == "" && msg.XMAUrl != "" {
		urlMatch = msg.XMAUrl
	}

	// Unwrap Facebook redirect URLs.
	if urlMatch != "" {
		urlMatch = unwrapFacebookURL(urlMatch)
	}

	if urlMatch == "" {
		return
	}

	b.Log.Debug().Str("url", urlMatch).Msg("[DEBUG] URL detected, processing media")

	timeout := time.Duration(b.Cfg.Performance.MediaCommandTimeoutSeconds) * time.Second
	autoCtx, autoCancel := context.WithTimeout(context.Background(), timeout)
	defer autoCancel()
	b.processMediaAuto(autoCtx, msg.ThreadKey, urlMatch)
}

// processMediaAuto downloads detected media and sends it to the thread.
func (b *Bot) processMediaAuto(ctx context.Context, threadID int64, rawURL string) {
	result, err := b.mediaService.GetMediaItems(ctx, rawURL)
	if err != nil {
		b.Log.Warn().Err(err).Str("url", rawURL).Msg("Auto-detect media failed")
		return
	}
	if len(result.Items) == 0 {
		return
	}

	b.Log.Info().Str("url", rawURL).Int("count", len(result.Items)).Msg("Auto-detected media")

	caption := result.Message
	results := b.mediaService.DownloadBatch(ctx, result.Items)

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
			b.Log.Error().Err(r.Err).Int("index", r.Index).Msg("Failed to download media")
			continue
		}
		file := r.File
		attachments = append(attachments, core.MediaAttachment{
			FilePath: file.Path,
			FileSize: file.Size,
			Filename: file.Filename,
			MimeType: file.MimeType,
		})
	}

	if len(attachments) == 0 {
		if caption != "" {
			b.sender.SendMessage(ctx, threadID, caption)
		}
		return
	}

	if err := b.sender.SendMultiMedia(ctx, threadID, attachments, caption); err != nil {
		b.Log.Error().Err(err).Msg("Failed to send media")
	}
}

// unwrapFacebookURL extracts the real URL from Facebook redirect wrappers
// like https://l.facebook.com/l.php?u=https%3A%2F%2Finstagram.com%2F...
func unwrapFacebookURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	host := strings.ToLower(u.Hostname())
	if (host == "l.facebook.com" || host == "lm.facebook.com") && u.Path == "/l.php" {
		if realURL := u.Query().Get("u"); realURL != "" {
			return realURL
		}
	}
	return rawURL
}
