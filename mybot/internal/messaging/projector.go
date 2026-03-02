package messaging

import (
	"context"
	"fmt"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix/table"

	"mybot/internal/core"
)

const (
	unknownThreadName = "(unknown thread)"
	unknownUserName   = "(unknown user)"
)

type ProjectionMode int

const (
	MetadataOnly ProjectionMode = iota
	FullEvents
)

type ProjectionResult struct {
	MissingThreadIDs map[int64]struct{}
	MissingUserIDs   map[int64]struct{}
	EditedMessageIDs map[string]struct{}
}

type Projector struct {
	store          Store
	selfIDProvider func() int64
	now            func() time.Time
}

func NewProjector(store Store, selfIDProvider func() int64) *Projector {
	return &Projector{
		store:          store,
		selfIDProvider: selfIDProvider,
		now:            time.Now,
	}
}

func (p *Projector) ProjectTable(ctx context.Context, tbl *table.LSTable, mode ProjectionMode) (*ProjectionResult, error) {
	if tbl == nil {
		return &ProjectionResult{
			MissingThreadIDs: make(map[int64]struct{}),
			MissingUserIDs:   make(map[int64]struct{}),
			EditedMessageIDs: make(map[string]struct{}),
		}, nil
	}

	result := &ProjectionResult{
		MissingThreadIDs: make(map[int64]struct{}),
		MissingUserIDs:   make(map[int64]struct{}),
		EditedMessageIDs: make(map[string]struct{}),
	}

	for _, row := range tbl.LSUpdateOrInsertThread {
		if err := p.upsertThread(ctx, row.ThreadKey, row.ThreadName, row.LastActivityTimestampMs, false); err != nil {
			return nil, err
		}
	}
	for _, row := range tbl.LSDeleteThenInsertThread {
		if err := p.upsertThread(ctx, row.ThreadKey, row.ThreadName, row.LastActivityTimestampMs, false); err != nil {
			return nil, err
		}
	}
	for _, row := range tbl.LSSyncUpdateThreadName {
		name := row.ThreadName
		if name == "" {
			name = row.ThreadName1
		}
		if err := p.upsertThread(ctx, row.ThreadKey, name, 0, false); err != nil {
			return nil, err
		}
	}
	for _, row := range tbl.LSVerifyThreadExists {
		if err := p.upsertThread(ctx, row.ThreadKey, "", 0, false); err != nil {
			return nil, err
		}
	}
	for _, row := range tbl.LSDeleteThread {
		if err := p.upsertThread(ctx, row.ThreadKey, "", 0, true); err != nil {
			return nil, err
		}
	}

	for _, row := range tbl.LSVerifyContactRowExists {
		if err := p.upsertUser(ctx, row.ContactId, row.Name, false); err != nil {
			return nil, err
		}
	}
	for _, row := range tbl.LSDeleteThenInsertContact {
		if err := p.upsertUser(ctx, row.Id, row.Name, false); err != nil {
			return nil, err
		}
	}
	for _, row := range tbl.LSVerifyContactParticipantExist {
		if err := p.upsertUser(ctx, row.ContactID, row.Name, false); err != nil {
			return nil, err
		}
	}

	if mode == MetadataOnly {
		return result, nil
	}

	upserts, inserts := tbl.WrapMessages()
	for _, grouped := range upserts {
		for _, wrapped := range grouped.Messages {
			if err := p.upsertWrappedMessage(ctx, wrapped, result); err != nil {
				return nil, err
			}
		}
	}
	for _, wrapped := range inserts {
		if err := p.upsertWrappedMessage(ctx, wrapped, result); err != nil {
			return nil, err
		}
	}

	for _, edit := range tbl.LSEditMessage {
		if err := p.applyEdit(ctx, edit, result); err != nil {
			return nil, err
		}
	}
	for _, deletion := range tbl.LSDeleteMessage {
		if err := p.applyDelete(ctx, deletion); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func (p *Projector) upsertThread(ctx context.Context, threadID int64, name string, lastActivityMs int64, deleted bool) error {
	if threadID == 0 {
		return nil
	}
	nowMs := p.now().UnixMilli()
	rec, err := p.store.GetThread(ctx, threadID)
	if err != nil {
		return err
	}
	if rec == nil {
		rec = &core.ThreadRecord{
			ThreadID: threadID,
			Name:     unknownThreadName,
		}
	}
	if name != "" {
		rec.Name = name
	}
	if rec.Name == "" {
		rec.Name = unknownThreadName
	}
	if lastActivityMs > 0 {
		rec.LastActivityMs = lastActivityMs
	}
	if deleted {
		rec.Deleted = true
	}
	rec.UpdatedAtUnixMs = nowMs
	return p.store.UpsertThread(ctx, rec)
}

func (p *Projector) upsertUser(ctx context.Context, userID int64, name string, deleted bool) error {
	if userID == 0 {
		return nil
	}
	nowMs := p.now().UnixMilli()
	rec, err := p.store.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if rec == nil {
		rec = &core.UserRecord{
			UserID: userID,
			Name:   unknownUserName,
		}
	}
	if name != "" {
		rec.Name = name
	}
	if rec.Name == "" {
		rec.Name = unknownUserName
	}
	if deleted {
		rec.Deleted = true
	}
	rec.UpdatedAtUnixMs = nowMs
	return p.store.UpsertUser(ctx, rec)
}

func (p *Projector) upsertWrappedMessage(ctx context.Context, wrapped *table.WrappedMessage, result *ProjectionResult) error {
	if wrapped == nil || wrapped.LSInsertMessage == nil || wrapped.MessageId == "" {
		return nil
	}

	if err := p.ensureThreadAndUser(ctx, wrapped.ThreadKey, wrapped.SenderId, result); err != nil {
		return err
	}

	user, err := p.store.GetUser(ctx, wrapped.SenderId)
	if err != nil {
		return err
	}

	nowMs := p.now().UnixMilli()
	rec := &core.MessageRecord{
		MessageID:          wrapped.MessageId,
		ThreadID:           wrapped.ThreadKey,
		SenderID:           wrapped.SenderId,
		SenderNameSnapshot: nameOrDefault(user, unknownUserName),
		Text:               wrapped.Text,
		ReplyToMessageID:   wrapped.ReplySourceId,
		OfflineThreadingID: wrapped.OfflineThreadingId,
		IsFromBot:          wrapped.SenderId != 0 && wrapped.SenderId == p.selfIDProvider(),
		HasMedia:           len(wrapped.Attachments) > 0 || len(wrapped.BlobAttachments) > 0 || len(wrapped.XMAAttachments) > 0 || len(wrapped.Stickers) > 0,
		Attachments:        attachmentMetaFromWrapped(wrapped),
		TimestampMs:        wrapped.TimestampMs,
		EditCount:          wrapped.EditCount,
		IsEdited:           wrapped.EditCount > 0,
		IsRecalled:         wrapped.IsUnsent,
		CreatedAtUnixMs:    nowMs,
		UpdatedAtUnixMs:    nowMs,
	}

	if existing, err := p.store.GetMessage(ctx, wrapped.MessageId); err != nil {
		return err
	} else if existing != nil {
		rec.CreatedAtUnixMs = existing.CreatedAtUnixMs
		if existing.RecalledAtUnixMs > 0 {
			rec.RecalledAtUnixMs = existing.RecalledAtUnixMs
		}
	}

	if err := p.store.UpsertMessage(ctx, rec); err != nil {
		return err
	}
	if rec.IsFromBot {
		if err := p.store.SetLastBotMessage(ctx, rec.ThreadID, rec.MessageID); err != nil {
			return err
		}
	}
	return nil
}

func (p *Projector) applyEdit(ctx context.Context, edit *table.LSEditMessage, result *ProjectionResult) error {
	if edit == nil || edit.MessageID == "" {
		return nil
	}
	rec, err := p.store.GetMessage(ctx, edit.MessageID)
	if err != nil {
		return err
	}
	if rec == nil {
		return nil
	}
	rec.Text = edit.Text
	rec.EditCount = edit.EditCount
	rec.IsEdited = true
	rec.UpdatedAtUnixMs = p.now().UnixMilli()
	if err := p.store.UpsertMessage(ctx, rec); err != nil {
		return err
	}
	result.EditedMessageIDs[rec.MessageID] = struct{}{}
	return nil
}

func (p *Projector) applyDelete(ctx context.Context, deletion *table.LSDeleteMessage) error {
	if deletion == nil || deletion.MessageId == "" {
		return nil
	}
	rec, err := p.store.GetMessage(ctx, deletion.MessageId)
	if err != nil {
		return err
	}
	if rec == nil {
		return nil
	}
	rec.IsRecalled = true
	rec.RecalledAtUnixMs = p.now().UnixMilli()
	rec.UpdatedAtUnixMs = rec.RecalledAtUnixMs
	if err := p.store.UpsertMessage(ctx, rec); err != nil {
		return err
	}
	return p.store.ClearLastBotMessage(ctx, rec.ThreadID, rec.MessageID)
}

func (p *Projector) ensureThreadAndUser(ctx context.Context, threadID, userID int64, result *ProjectionResult) error {
	threadRec, err := p.store.GetThread(ctx, threadID)
	if err != nil {
		return err
	}
	if threadRec == nil {
		if err := p.upsertThread(ctx, threadID, "", 0, false); err != nil {
			return err
		}
		result.MissingThreadIDs[threadID] = struct{}{}
	}

	userRec, err := p.store.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if userRec == nil {
		if err := p.upsertUser(ctx, userID, "", false); err != nil {
			return err
		}
		result.MissingUserIDs[userID] = struct{}{}
	}
	return nil
}

func attachmentMetaFromWrapped(wrapped *table.WrappedMessage) []core.AttachmentMeta {
	var items []core.AttachmentMeta
	for _, att := range wrapped.Attachments {
		items = append(items, core.AttachmentMeta{
			AttachmentID: att.AttachmentFbid,
			Kind:         fmt.Sprintf("attachment:%d", att.AttachmentType),
			Filename:     att.Filename,
			MimeType:     firstNonEmpty(att.AttachmentMimeType, att.PlayableUrlMimeType, att.PreviewUrlMimeType, att.ImageUrlMimeType),
			SizeBytes:    att.Filesize,
		})
	}
	for _, att := range wrapped.BlobAttachments {
		items = append(items, core.AttachmentMeta{
			AttachmentID: att.AttachmentFbid,
			Kind:         fmt.Sprintf("blob:%d", att.AttachmentType),
			Filename:     att.Filename,
			MimeType:     firstNonEmpty(att.AttachmentMimeType, att.PlayableUrlMimeType, att.PreviewUrlMimeType),
			SizeBytes:    att.Filesize,
		})
	}
	for _, att := range wrapped.XMAAttachments {
		items = append(items, core.AttachmentMeta{
			AttachmentID: att.AttachmentFbid,
			Kind:         fmt.Sprintf("xma:%d", att.AttachmentType),
			Filename:     att.Filename,
			MimeType:     firstNonEmpty(att.PlayableUrlMimeType, att.PreviewUrlMimeType),
			SizeBytes:    att.Filesize,
		})
	}
	for _, att := range wrapped.Stickers {
		items = append(items, core.AttachmentMeta{
			AttachmentID: att.AttachmentFbid,
			Kind:         "sticker",
			MimeType:     firstNonEmpty(att.PlayableUrlMimeType, att.PreviewUrlMimeType, att.ImageUrlMimeType),
		})
	}
	return items
}

func nameOrDefault(rec *core.UserRecord, fallback string) string {
	if rec != nil && rec.Name != "" {
		return rec.Name
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
