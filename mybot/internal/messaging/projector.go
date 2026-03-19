package messaging

import (
	"context"
	"fmt"
	"sync"
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

// ── LRU existence cache ─────────────────────────────────────────────────────

// existenceCache is a simple bounded set that remembers whether a thread/user
// exists to avoid redundant DB reads during projection.
type existenceCache struct {
	mu       sync.Mutex
	threads  map[int64]struct{}
	users    map[int64]struct{}
	maxItems int
}

func newExistenceCache(maxItems int) *existenceCache {
	if maxItems <= 0 {
		maxItems = 2048
	}
	return &existenceCache{
		threads:  make(map[int64]struct{}, 256),
		users:    make(map[int64]struct{}, 256),
		maxItems: maxItems,
	}
}

func (c *existenceCache) hasThread(id int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.threads[id]
	return ok
}

func (c *existenceCache) addThread(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.threads) >= c.maxItems {
		// Rebuild the map to release memory from deleted entries.
		// Go maps never shrink their backing array, so after many
		// deletions the old map still holds pages. A fresh map with
		// half capacity uses far less RAM.
		keepCount := c.maxItems / 2
		newMap := make(map[int64]struct{}, keepCount)
		i := 0
		for k := range c.threads {
			if i >= keepCount {
				break
			}
			newMap[k] = struct{}{}
			i++
		}
		c.threads = newMap
	}
	c.threads[id] = struct{}{}
}

func (c *existenceCache) hasUser(id int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.users[id]
	return ok
}

func (c *existenceCache) addUser(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.users) >= c.maxItems {
		keepCount := c.maxItems / 2
		newMap := make(map[int64]struct{}, keepCount)
		i := 0
		for k := range c.users {
			if i >= keepCount {
				break
			}
			newMap[k] = struct{}{}
			i++
		}
		c.users = newMap
	}
	c.users[id] = struct{}{}
}

// ── Projector ───────────────────────────────────────────────────────────────

type Projector struct {
	store          Store
	selfIDProvider func() int64
	now            func() time.Time
	cache          *existenceCache
}

func NewProjector(store Store, selfIDProvider func() int64) *Projector {
	return &Projector{
		store:          store,
		selfIDProvider: selfIDProvider,
		now:            time.Now,
		cache:          newExistenceCache(2048),
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

	// ── Metadata: threads ───────────────────────────────────────────────
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

	// ── Metadata: users ─────────────────────────────────────────────────
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

	// ── Messages (batched) ──────────────────────────────────────────────
	upserts, inserts := tbl.WrapMessages()

	// Pre-collect all wrapped messages.
	var allWrapped []*table.WrappedMessage
	for _, grouped := range upserts {
		allWrapped = append(allWrapped, grouped.Messages...)
	}
	allWrapped = append(allWrapped, inserts...)

	// Ensure thread/user existence (using cache to skip DB reads).
	for _, w := range allWrapped {
		if w == nil || w.LSInsertMessage == nil || w.MessageId == "" {
			continue
		}
		if err := p.ensureThreadAndUser(ctx, w.ThreadKey, w.SenderId, result); err != nil {
			return nil, err
		}
	}

	// Build user name cache for this batch to avoid per-message lookups.
	userNames := make(map[int64]string)
	for _, w := range allWrapped {
		if w == nil || w.SenderId == 0 {
			continue
		}
		if _, ok := userNames[w.SenderId]; !ok {
			user, err := p.store.GetUser(ctx, w.SenderId)
			if err != nil {
				return nil, err
			}
			userNames[w.SenderId] = nameOrDefault(user, unknownUserName)
		}
	}

	// Project messages.
	selfID := p.selfIDProvider()
	nowMs := p.now().UnixMilli()
	for _, wrapped := range allWrapped {
		if wrapped == nil || wrapped.LSInsertMessage == nil || wrapped.MessageId == "" {
			continue
		}

		senderName := userNames[wrapped.SenderId]
		if senderName == "" {
			senderName = unknownUserName
		}

		rec := &core.MessageRecord{
			MessageID:          wrapped.MessageId,
			ThreadID:           wrapped.ThreadKey,
			SenderID:           wrapped.SenderId,
			SenderNameSnapshot: senderName,
			Text:               wrapped.Text,
			ReplyToMessageID:   wrapped.ReplySourceId,
			OfflineThreadingID: wrapped.OfflineThreadingId,
			IsFromBot:          wrapped.SenderId != 0 && wrapped.SenderId == selfID,
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
			return nil, err
		} else if existing != nil {
			rec.CreatedAtUnixMs = existing.CreatedAtUnixMs
			if existing.RecalledAtUnixMs > 0 {
				rec.RecalledAtUnixMs = existing.RecalledAtUnixMs
			}
		}

		if err := p.store.UpsertMessage(ctx, rec); err != nil {
			return nil, err
		}
		if rec.IsFromBot {
			if err := p.store.SetLastBotMessage(ctx, rec.ThreadID, rec.MessageID); err != nil {
				return nil, err
			}
		}
	}

	// ── Edits ───────────────────────────────────────────────────────────
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
	if err := p.store.UpsertThread(ctx, rec); err != nil {
		return err
	}
	p.cache.addThread(threadID)
	return nil
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
	if err := p.store.UpsertUser(ctx, rec); err != nil {
		return err
	}
	p.cache.addUser(userID)
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
	// Use cache to skip DB lookups for known entities.
	if !p.cache.hasThread(threadID) {
		threadRec, err := p.store.GetThread(ctx, threadID)
		if err != nil {
			return err
		}
		if threadRec == nil {
			if err := p.upsertThread(ctx, threadID, "", 0, false); err != nil {
				return err
			}
			result.MissingThreadIDs[threadID] = struct{}{}
		} else {
			p.cache.addThread(threadID)
		}
	}

	if !p.cache.hasUser(userID) {
		userRec, err := p.store.GetUser(ctx, userID)
		if err != nil {
			return err
		}
		if userRec == nil {
			if err := p.upsertUser(ctx, userID, "", false); err != nil {
				return err
			}
			result.MissingUserIDs[userID] = struct{}{}
		} else {
			p.cache.addUser(userID)
		}
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
