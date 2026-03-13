package app

import (
	"context"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix"

	"mybot/internal/messaging"
	"mybot/internal/metrics"
)

// handleEvent is the Messagix event callback. It dispatches incoming
// WebSocket events to the appropriate handler methods.
func (b *Bot) handleEvent(ctx context.Context, evt any) {
	switch e := evt.(type) {
	case *messagix.Event_Ready:
		b.connectTime.Store(time.Now().UnixMilli())
		b.botReady.Store(true)
		b.Log.Info().Msg("Bot is ready to process messages")

	case *messagix.Event_Reconnected:
		b.connectTime.Store(time.Now().UnixMilli())
		b.botReady.Store(true)
		b.Log.Info().Msg("Bot reconnected, ready to process messages")

	case *messagix.Event_PublishResponse:
		b.handlePublishResponse(ctx, e)

	case *messagix.Event_SocketError:
		b.Log.Error().Err(e.Err).Int("attempts", e.ConnectionAttempts).Msg("Socket error")
		if e.ConnectionAttempts >= 10 {
			b.Log.Warn().Msg("Too many failed reconnect attempts, triggering full reconnect")
			b.triggerReconnect()
		}

	case *messagix.Event_PermanentError:
		b.Log.Error().Err(e.Err).Msg("Permanent connection error, triggering full reconnect in 30s")
		b.botReady.Store(false)
		go func() {
			time.Sleep(30 * time.Second)
			b.triggerReconnect()
		}()
	}
}

// handlePublishResponse processes table updates from the WebSocket stream.
func (b *Bot) handlePublishResponse(ctx context.Context, e *messagix.Event_PublishResponse) {
	if e.Table == nil {
		return
	}

	if err := b.messageAPI.ObserveTable(ctx, e.Table, messaging.FullEvents); err != nil {
		b.Log.Warn().Err(err).Msg("Failed to project table update")
	}

	if !b.botReady.Load() {
		return
	}

	b.Log.Info().
		Int("upsert_message_count", len(e.Table.LSUpsertMessage)).
		Int("insert_message_count", len(e.Table.LSInsertMessage)).
		Int("xma_attachment_count", len(e.Table.LSInsertXmaAttachment)).
		Int("attachment_cta_count", len(e.Table.LSInsertAttachmentCta)).
		Msg("Received table update")

	// Build XMA URL map: messageId → actionUrl.
	xmaURLs := b.buildXMAMap(e)

	// Dispatch all upsert messages.
	for _, m := range e.Table.LSUpsertMessage {
		b.submitMessage(m.ThreadKey, m.Text, m.SenderId, m.MessageId, m.TimestampMs, m.TextHasLinks, xmaURLs)
	}

	// Dispatch all insert messages.
	for _, m := range e.Table.LSInsertMessage {
		b.submitMessage(m.ThreadKey, m.Text, m.SenderId, m.MessageId, m.TimestampMs, m.TextHasLinks, xmaURLs)
	}
}

// submitMessage wraps a raw LS message and submits it to the worker pool.
func (b *Bot) submitMessage(threadKey int64, text string, senderID int64, messageID string, timestampMs int64, textHasLinks bool, xmaURLs map[string]string) {
	msg := &WrappedMessage{
		ThreadKey:    threadKey,
		Text:         text,
		SenderId:     senderID,
		MessageId:    messageID,
		TimestampMs:  timestampMs,
		TextHasLinks: textHasLinks,
		XMAUrl:       xmaURLs[messageID],
	}
	metrics.Global.MessagesReceived.Add(1)
	b.workerPool.Submit(func() {
		b.handleMessage(msg)
	})
}

// buildXMAMap extracts message ID → action URL from XMA attachments and CTAs.
func (b *Bot) buildXMAMap(e *messagix.Event_PublishResponse) map[string]string {
	xmaURLs := make(map[string]string)

	for _, xma := range e.Table.LSInsertXmaAttachment {
		b.Log.Debug().
			Str("msg_id", xma.MessageId).
			Int64("thread", xma.ThreadKey).
			Str("action_url", xma.ActionUrl).
			Str("title", xma.TitleText).
			Str("subtitle", xma.SubtitleText).
			Str("source", xma.SourceText).
			Msg("[DEBUG] XMA attachment")
		if xma.ActionUrl != "" && xma.MessageId != "" {
			xmaURLs[xma.MessageId] = xma.ActionUrl
		}
	}

	for _, cta := range e.Table.LSInsertAttachmentCta {
		b.Log.Debug().
			Str("msg_id", cta.MessageId).
			Int64("thread", cta.ThreadKey).
			Str("action_url", cta.ActionUrl).
			Str("native_url", cta.NativeUrl).
			Str("title", cta.Title).
			Msg("[DEBUG] Attachment CTA")
		if cta.MessageId != "" {
			if cta.ActionUrl != "" {
				xmaURLs[cta.MessageId] = cta.ActionUrl
			} else if cta.NativeUrl != "" {
				xmaURLs[cta.MessageId] = cta.NativeUrl
			}
		}
	}

	return xmaURLs
}
