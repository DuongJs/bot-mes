package messaging

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-meta/pkg/messagix"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"

	"mybot/internal/core"
)

type Service struct {
	log              zerolog.Logger
	store            Store
	projector        *Projector
	tracker          *Tracker
	transportFactory func() Transport
	clientFactory    func() *messagix.Client
	rateLimiter      *RateLimiter

	refreshMu            sync.Mutex
	lastMetadataRefresh  time.Time
	metadataRefreshEvery time.Duration
}

func NewService(log zerolog.Logger, store Store, selfIDProvider func() int64, transportFactory func() Transport, clientFactory func() *messagix.Client, rateLimiterOpts ...RateLimiterOpt) *Service {
	tracker := NewTracker()
	rl := NewRateLimiter(30, 10) // defaults
	for _, opt := range rateLimiterOpts {
		opt(rl)
	}
	return &Service{
		log:                  log,
		store:                store,
		projector:            NewProjector(store, selfIDProvider),
		tracker:              tracker,
		transportFactory:     transportFactory,
		clientFactory:        clientFactory,
		rateLimiter:          rl,
		metadataRefreshEvery: 60 * time.Second,
	}
}

// RateLimiterOpt configures the rate limiter on the service.
type RateLimiterOpt func(*RateLimiter)

// WithRateLimit sets the rate limiter parameters.
func WithRateLimit(ratePerSec, burst int) RateLimiterOpt {
	return func(rl *RateLimiter) {
		*rl = *NewRateLimiter(ratePerSec, burst)
	}
}

func (s *Service) Tracker() *Tracker {
	return s.tracker
}

func (s *Service) Close() error {
	return s.store.Close()
}

func (s *Service) ObserveTable(ctx context.Context, tbl *table.LSTable, mode ProjectionMode) error {
	result, err := s.projector.ProjectTable(ctx, tbl, mode)
	if err != nil {
		return err
	}
	for messageID := range result.EditedMessageIDs {
		rec, getErr := s.store.GetMessage(ctx, messageID)
		if getErr == nil && rec != nil {
			s.tracker.NotifyEdit(rec)
		}
	}
	if mode == FullEvents {
		s.refreshMissingMetadata(result)
	}
	return nil
}

func (s *Service) SelfID() int64 {
	transport := s.transportFactory()
	if transport == nil {
		return 0
	}
	return transport.GetSelfID()
}

func (s *Service) SendText(ctx context.Context, req core.SendTextRequest) (*core.MessageRecord, error) {
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limited: %w", err)
	}
	transport, err := s.transport()
	if err != nil {
		return nil, err
	}
	rec, err := transport.SendText(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.persistSentMessage(ctx, rec, transport.GetSelfID())
}

func (s *Service) SendMedia(ctx context.Context, req core.SendMediaRequest) (*core.MessageRecord, error) {
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limited: %w", err)
	}
	transport, err := s.transport()
	if err != nil {
		return nil, err
	}
	rec, err := transport.SendMediaMessage(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.persistSentMessage(ctx, rec, transport.GetSelfID())
}

func (s *Service) ReplyText(ctx context.Context, threadID int64, replyToMessageID, text string) (*core.MessageRecord, error) {
	rec, err := s.SendText(ctx, core.SendTextRequest{
		ThreadID: threadID,
		Text:     text,
		ReplyTo: &core.ReplyTarget{
			MessageID: replyToMessageID,
		},
	})
	if rec != nil && rec.ReplyToMessageID == "" {
		rec.ReplyToMessageID = replyToMessageID
		if updateErr := s.store.UpsertMessage(ctx, rec); updateErr != nil {
			return nil, updateErr
		}
	}
	return rec, err
}

func (s *Service) EditText(ctx context.Context, messageID, newText string) (*core.MessageRecord, error) {
	if messageID == "" {
		return nil, ErrMessageNotFound
	}
	existing, err := s.store.GetMessage(ctx, messageID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, ErrMessageNotFound
	}

	transport, err := s.transport()
	if err != nil {
		return nil, err
	}
	rec, err := transport.EditText(ctx, messageID, newText)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, err
		}
		return nil, err
	}
	if rec == nil {
		return nil, ErrEditNotConfirmed
	}

	if existing.ThreadID != 0 && rec.ThreadID == 0 {
		rec.ThreadID = existing.ThreadID
	}
	if existing.SenderID != 0 && rec.SenderID == 0 {
		rec.SenderID = existing.SenderID
	}
	rec.CreatedAtUnixMs = existing.CreatedAtUnixMs
	rec.UpdatedAtUnixMs = time.Now().UnixMilli()
	rec.IsFromBot = existing.IsFromBot
	if err := s.store.UpsertMessage(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

func (s *Service) Recall(ctx context.Context, messageID string) error {
	rec, err := s.store.GetMessage(ctx, messageID)
	if err != nil {
		return err
	}
	if rec == nil {
		return ErrMessageNotFound
	}

	transport, err := s.transport()
	if err != nil {
		return err
	}
	if err := transport.Recall(ctx, messageID); err != nil {
		return err
	}

	rec.IsRecalled = true
	rec.RecalledAtUnixMs = time.Now().UnixMilli()
	rec.UpdatedAtUnixMs = rec.RecalledAtUnixMs
	if err := s.store.UpsertMessage(ctx, rec); err != nil {
		return err
	}
	return s.store.ClearLastBotMessage(ctx, rec.ThreadID, rec.MessageID)
}

func (s *Service) GetMessage(ctx context.Context, messageID string) (*core.MessageRecord, error) {
	return s.store.GetMessage(ctx, messageID)
}

func (s *Service) GetLastBotMessage(ctx context.Context, threadID int64) (*core.MessageRecord, error) {
	rec, err := s.store.GetLastBotMessage(ctx, threadID)
	if err != nil || rec == nil {
		return rec, err
	}
	if rec.IsRecalled {
		if clearErr := s.store.ClearLastBotMessage(ctx, threadID, rec.MessageID); clearErr != nil {
			return nil, clearErr
		}
		return nil, nil
	}
	return rec, nil
}

func (s *Service) GetThread(ctx context.Context, threadID int64) (*core.ThreadRecord, error) {
	return s.store.GetThread(ctx, threadID)
}

func (s *Service) GetUser(ctx context.Context, userID int64) (*core.UserRecord, error) {
	return s.store.GetUser(ctx, userID)
}

func (s *Service) ListThreadMessages(ctx context.Context, threadID int64, limit int, beforeMessageID string) ([]*core.MessageRecord, error) {
	return s.store.ListThreadMessages(ctx, threadID, limit, beforeMessageID)
}

func (s *Service) persistSentMessage(ctx context.Context, rec *core.MessageRecord, selfID int64) (*core.MessageRecord, error) {
	if rec == nil {
		return nil, nil
	}
	nowMs := time.Now().UnixMilli()
	if rec.CreatedAtUnixMs == 0 {
		rec.CreatedAtUnixMs = nowMs
	}
	rec.UpdatedAtUnixMs = nowMs
	rec.IsFromBot = true
	if rec.SenderID == 0 {
		rec.SenderID = selfID
	}

	if err := s.ensurePlaceholders(ctx, rec.ThreadID, rec.SenderID); err != nil {
		return nil, err
	}
	if user, err := s.store.GetUser(ctx, rec.SenderID); err == nil && user != nil && rec.SenderNameSnapshot == "" {
		rec.SenderNameSnapshot = user.Name
	}
	if rec.SenderNameSnapshot == "" {
		rec.SenderNameSnapshot = unknownUserName
	}
	if err := s.store.UpsertMessage(ctx, rec); err != nil {
		return nil, err
	}
	if err := s.store.SetLastBotMessage(ctx, rec.ThreadID, rec.MessageID); err != nil {
		return nil, err
	}
	return rec, nil
}

func (s *Service) ensurePlaceholders(ctx context.Context, threadID, userID int64) error {
	nowMs := time.Now().UnixMilli()
	threadRec, err := s.store.GetThread(ctx, threadID)
	if err != nil {
		return err
	}
	if threadRec == nil && threadID != 0 {
		if err := s.store.UpsertThread(ctx, &core.ThreadRecord{
			ThreadID:        threadID,
			Name:            unknownThreadName,
			UpdatedAtUnixMs: nowMs,
		}); err != nil {
			return err
		}
	}
	userRec, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if userRec == nil && userID != 0 {
		if err := s.store.UpsertUser(ctx, &core.UserRecord{
			UserID:          userID,
			Name:            unknownUserName,
			UpdatedAtUnixMs: nowMs,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) transport() (Transport, error) {
	transport := s.transportFactory()
	if transport == nil {
		return nil, ErrTransportUnavailable
	}
	return transport, nil
}

type LegacySender struct {
	service *Service
}

func NewLegacySender(service *Service) *LegacySender {
	return &LegacySender{service: service}
}

func (s *LegacySender) SendMessage(ctx context.Context, threadID int64, text string) error {
	_, err := s.service.SendText(ctx, core.SendTextRequest{ThreadID: threadID, Text: text})
	return err
}

func (s *LegacySender) SendMedia(ctx context.Context, threadID int64, data []byte, filename, mimeType string, caption ...string) error {
	text := ""
	if len(caption) > 0 {
		text = caption[0]
	}
	_, err := s.service.SendMedia(ctx, core.SendMediaRequest{
		ThreadID: threadID,
		Items: []core.MediaAttachment{{
			Data:     data,
			Filename: filename,
			MimeType: mimeType,
		}},
		Text: text,
	})
	return err
}

func (s *LegacySender) SendMultiMedia(ctx context.Context, threadID int64, items []core.MediaAttachment, caption ...string) error {
	text := ""
	if len(caption) > 0 {
		text = caption[0]
	}
	_, err := s.service.SendMedia(ctx, core.SendMediaRequest{
		ThreadID: threadID,
		Items:    items,
		Text:     text,
	})
	return err
}

func (s *LegacySender) GetSelfID() int64 {
	return s.service.SelfID()
}

func (s *Service) refreshMissingMetadata(result *ProjectionResult) {
	if result == nil {
		return
	}
	if len(result.MissingThreadIDs) == 0 && len(result.MissingUserIDs) == 0 {
		return
	}

	for userID := range result.MissingUserIDs {
		go s.fetchUserMetadata(userID)
	}
	if len(result.MissingThreadIDs) > 0 {
		go s.refreshMetadata()
	}
}

func (s *Service) fetchUserMetadata(userID int64) {
	if userID == 0 || s.clientFactory == nil {
		return
	}
	client := s.clientFactory()
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tbl, err := client.ExecuteTask(ctx, &socket.GetContactsFullTask{ContactID: userID})
	if err != nil {
		s.log.Debug().Err(err).Int64("user_id", userID).Msg("Failed to refresh user metadata")
		return
	}
	if err := s.ObserveTable(ctx, tbl, MetadataOnly); err != nil {
		s.log.Debug().Err(err).Int64("user_id", userID).Msg("Failed to project refreshed user metadata")
	}
}

func (s *Service) refreshMetadata() {
	if s.clientFactory == nil {
		return
	}

	s.refreshMu.Lock()
	if !s.lastMetadataRefresh.IsZero() && time.Since(s.lastMetadataRefresh) < s.metadataRefreshEvery {
		s.refreshMu.Unlock()
		return
	}
	s.lastMetadataRefresh = time.Now()
	s.refreshMu.Unlock()

	client := s.clientFactory()
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, tbl, err := client.LoadMessagesPage(ctx)
	if err != nil {
		s.log.Debug().Err(err).Msg("Failed to refresh thread metadata")
		return
	}
	if err := s.ObserveTable(ctx, tbl, MetadataOnly); err != nil {
		s.log.Debug().Err(err).Msg("Failed to project refreshed thread metadata")
	}
}
