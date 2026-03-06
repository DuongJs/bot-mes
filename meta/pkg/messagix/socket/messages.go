package socket

import (
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

type SendReactionTask struct {
	ThreadKey       int64                  `json:"thread_key,omitempty"`
	TimestampMs     int64                  `json:"timestamp_ms"`
	MessageID       string                 `json:"message_id"`
	ActorID         int64                  `json:"actor_id"`
	Reaction        string                 `json:"reaction"` // unicode emoji (empty reaction to remove)
	ReactionStyle   interface{}            `json:"reaction_style"`
	SyncGroup       int                    `json:"sync_group"`
	SendAttribution table.ThreadSourceType `json:"send_attribution"`
}

func (t *SendReactionTask) GetLabel() string {
	return TaskLabels["SendReactionTask"]
}

func (t *SendReactionTask) Create() (interface{}, interface{}, bool) {
	t.TimestampMs = time.Now().UnixMilli()
	t.SyncGroup = 1
	queueName := []string{"reaction", t.MessageID}
	return t, queueName, true
}

type DeleteMessageTask struct {
	MessageId string `json:"message_id"`
}

func (t *DeleteMessageTask) GetLabel() string {
	return TaskLabels["DeleteMessageTask"]
}

func (t *DeleteMessageTask) Create() (interface{}, interface{}, bool) {
	queueName := "unsend_message"
	return t, queueName, false
}

type DeleteMessageMeOnlyTask struct {
	ThreadKey int64  `json:"thread_key,omitempty"`
	MessageId string `json:"message_id"`
}

func (t *DeleteMessageMeOnlyTask) GetLabel() string {
	return TaskLabels["DeleteMessageMeOnlyTask"]
}

func (t *DeleteMessageMeOnlyTask) Create() (interface{}, interface{}, bool) {
	queueName := "155"
	return t, queueName, false
}

type FetchReactionsV2UserList struct {
	ThreadID     int64   `json:"thread_id"`
	MessageID    string  `json:"message_id"`
	ReactionFBID *int64  `json:"reaction_fbid"`
	Cursor       *string `json:"cursor"`
	SyncGroup    int64   `json:"sync_group"`
}

func (t *FetchReactionsV2UserList) GetLabel() string {
	return TaskLabels["FetchReactionsV2UserList"]
}

func (t *FetchReactionsV2UserList) Create() (any, any, bool) {
	return t, "fetch_reactions_v2_details_users_list", false
}

type SendReactionV2Task struct {
	ThreadID         int64  `json:"thread_id"`
	MessageID        string `json:"message_id"`
	MessageTimestamp int64  `json:"message_timestamp"`
	ActorID          int64  `json:"actor_id"`
	ReactionFBID     int64  `json:"reaction_fbid"`
	ReactionStyle    int    `json:"reaction_style"` // 1
	CurrentCount     int    `json:"current_count"`
	ViewerIsReactor  int    `json:"viewer_is_reactor"` // 1 if adding reaction, 0 if removing reaction
	Operation        int    `json:"operation"`         // 1 for add, 3 for remove
	ReactionLiteral  string `json:"reaction_literal"`  // unicode emoji
	EntryPoint       any    `json:"entry_point"`       // null
	SyncGroup        int    `json:"sync_group"`        // 104
}

func (t *SendReactionV2Task) GetLabel() string {
	return TaskLabels["SendReactionV2"]
}

func (t *SendReactionV2Task) Create() (any, any, bool) {
	return t, []string{"reaction_v2", t.MessageID}, true
}

// ShareContactTask shares a contact card in a Messenger thread (label 359).
// Ported from JS FCA shareContact.js.
type ShareContactTask struct {
	ContactID int64  `json:"contact_id"`
	SyncGroup int64  `json:"sync_group"`
	Text      string `json:"text"`
	ThreadID  int64  `json:"thread_id"`
}

func (t *ShareContactTask) GetLabel() string {
	return TaskLabels["ShareContactTask"]
}

func (t *ShareContactTask) Create() (any, any, bool) {
	return t, "messenger_contact_sharing", false
}

// ChangeNicknameTask changes a participant's nickname in a thread (label 44).
// Ported from JS FCA changeNickname.js.
type ChangeNicknameTask struct {
	ThreadKey int64  `json:"thread_key"`
	ContactID int64  `json:"contact_id"`
	Nickname  string `json:"nickname"`
	SyncGroup int64  `json:"sync_group"`
}

func (t *ChangeNicknameTask) GetLabel() string {
	return TaskLabels["ChangeNicknameTask"]
}

func (t *ChangeNicknameTask) Create() (any, any, bool) {
	return t, "thread_participant_nickname", false
}

// ChangeThreadColorTask changes a thread's color/theme (label 43).
// The ThemeFBID is the Facebook theme identifier (e.g. from threadColors map).
// Ported from JS FCA changeThreadColor.js.
type ChangeThreadColorTask struct {
	ThreadKey int64       `json:"thread_key"`
	ThemeFBID string      `json:"theme_fbid"`
	Source    interface{} `json:"source"`
	SyncGroup int64       `json:"sync_group"`
	Payload   interface{} `json:"payload"`
}

func (t *ChangeThreadColorTask) GetLabel() string {
	return TaskLabels["ChangeThreadColorTask"]
}

func (t *ChangeThreadColorTask) Create() (any, any, bool) {
	return t, "thread_theme", false
}

// ChangeThreadEmojiTask changes the default quick-reaction emoji for a thread (label 100003).
// Ported from JS FCA changeThreadEmoji.js.
type ChangeThreadEmojiTask struct {
	ThreadKey                        int64       `json:"thread_key"`
	CustomEmoji                      string      `json:"custom_emoji"`
	AvatarStickerInstructionKeyID    interface{} `json:"avatar_sticker_instruction_key_id"`
	SyncGroup                        int64       `json:"sync_group"`
}

func (t *ChangeThreadEmojiTask) GetLabel() string {
	return TaskLabels["ChangeThreadEmojiTask"]
}

func (t *ChangeThreadEmojiTask) Create() (any, any, bool) {
	return t, "thread_quick_reaction", false
}

// TypingIndicatorTask sends a typing indicator to a thread (label 3).
// This is a stateless task (type 4) — Create returns nil for queue_name.
// Ported from JS FCA sendTypingIndicator.js.
type TypingIndicatorTask struct {
	ThreadKey     int64 `json:"thread_key"`
	IsGroupThread int   `json:"is_group_thread"`
	IsTyping      int   `json:"is_typing"`
	Attribution   int   `json:"attribution"`
	SyncGroup     int   `json:"sync_group"`
	ThreadType    int   `json:"thread_type"`
}

func (t *TypingIndicatorTask) GetLabel() string {
	return TaskLabels["TypingIndicatorTask"]
}

// Create returns nil queue_name, indicating this is a stateless task (type 4).
func (t *TypingIndicatorTask) Create() (any, any, bool) {
	return t, nil, false
}
