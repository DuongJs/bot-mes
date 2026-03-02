package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"

	"mybot/internal/core"
)

var (
	threadsBucket      = []byte("threads")
	usersBucket        = []byte("users")
	messagesBucket     = []byte("messages")
	threadMessagesBuck = []byte("thread_messages")
	threadLastBotBuck  = []byte("thread_last_bot")
	metaBucket         = []byte("meta")
)

type BoltStore struct {
	db *bolt.DB
}

func OpenBoltStore(path string) (*BoltStore, error) {
	if path == "" {
		return nil, fmt.Errorf("empty bolt db path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, err
	}

	store := &BoltStore{db: db}
	err = store.db.Update(func(tx *bolt.Tx) error {
		for _, bucket := range [][]byte{
			threadsBucket,
			usersBucket,
			messagesBucket,
			threadMessagesBuck,
			threadLastBotBuck,
			metaBucket,
		} {
			if _, err := tx.CreateBucketIfNotExists(bucket); err != nil {
				return err
			}
		}
		return tx.Bucket(metaBucket).Put([]byte("schema_version"), []byte("1"))
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *BoltStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *BoltStore) UpsertThread(_ context.Context, rec *core.ThreadRecord) error {
	if rec == nil || rec.ThreadID == 0 {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return putJSON(tx.Bucket(threadsBucket), int64Key(rec.ThreadID), rec)
	})
}

func (s *BoltStore) GetThread(_ context.Context, threadID int64) (*core.ThreadRecord, error) {
	var rec *core.ThreadRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		return getJSON(tx.Bucket(threadsBucket), int64Key(threadID), &rec)
	})
	return rec, err
}

func (s *BoltStore) UpsertUser(_ context.Context, rec *core.UserRecord) error {
	if rec == nil || rec.UserID == 0 {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return putJSON(tx.Bucket(usersBucket), int64Key(rec.UserID), rec)
	})
}

func (s *BoltStore) GetUser(_ context.Context, userID int64) (*core.UserRecord, error) {
	var rec *core.UserRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		return getJSON(tx.Bucket(usersBucket), int64Key(userID), &rec)
	})
	return rec, err
}

func (s *BoltStore) UpsertMessage(_ context.Context, rec *core.MessageRecord) error {
	if rec == nil || rec.MessageID == "" {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		msgBucket := tx.Bucket(messagesBucket)
		indexBucket := tx.Bucket(threadMessagesBuck)

		var existing *core.MessageRecord
		if err := getJSON(msgBucket, []byte(rec.MessageID), &existing); err != nil {
			return err
		}
		if existing != nil {
			if err := indexBucket.Delete(messageIndexKey(existing.ThreadID, existing.TimestampMs, existing.MessageID)); err != nil {
				return err
			}
		}

		if err := putJSON(msgBucket, []byte(rec.MessageID), rec); err != nil {
			return err
		}
		return indexBucket.Put(messageIndexKey(rec.ThreadID, rec.TimestampMs, rec.MessageID), []byte(rec.MessageID))
	})
}

func (s *BoltStore) GetMessage(_ context.Context, messageID string) (*core.MessageRecord, error) {
	if messageID == "" {
		return nil, nil
	}
	var rec *core.MessageRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		return getJSON(tx.Bucket(messagesBucket), []byte(messageID), &rec)
	})
	return rec, err
}

func (s *BoltStore) ListThreadMessages(ctx context.Context, threadID int64, limit int, beforeMessageID string) ([]*core.MessageRecord, error) {
	if limit <= 0 {
		limit = 50
	}

	var beforeRec *core.MessageRecord
	var err error
	if beforeMessageID != "" {
		beforeRec, err = s.GetMessage(ctx, beforeMessageID)
		if err != nil {
			return nil, err
		}
	}

	results := make([]*core.MessageRecord, 0, limit)
	err = s.db.View(func(tx *bolt.Tx) error {
		indexBucket := tx.Bucket(threadMessagesBuck)
		msgBucket := tx.Bucket(messagesBucket)
		cursor := indexBucket.Cursor()
		prefix := threadPrefix(threadID)

		var k, v []byte
		if beforeRec != nil {
			seekKey := messageIndexKey(beforeRec.ThreadID, beforeRec.TimestampMs, beforeRec.MessageID)
			k, v = cursor.Seek(seekKey)
			if k != nil && string(k) == string(seekKey) {
				k, v = cursor.Prev()
			} else if k == nil {
				k, v = cursor.Last()
			} else if strings.Compare(string(k), string(seekKey)) > 0 {
				k, v = cursor.Prev()
			}
		} else {
			k, v = cursor.Last()
		}

		for ; k != nil && len(results) < limit; k, v = cursor.Prev() {
			if !strings.HasPrefix(string(k), prefix) {
				if len(results) > 0 && strings.Compare(string(k), prefix) < 0 {
					break
				}
				continue
			}
			var rec *core.MessageRecord
			if err := getJSON(msgBucket, v, &rec); err != nil {
				return err
			}
			if rec != nil {
				results = append(results, rec)
			}
		}
		return nil
	})
	return results, err
}

func (s *BoltStore) SetLastBotMessage(_ context.Context, threadID int64, messageID string) error {
	if threadID == 0 || messageID == "" {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(threadLastBotBuck).Put(int64Key(threadID), []byte(messageID))
	})
}

func (s *BoltStore) GetLastBotMessage(ctx context.Context, threadID int64) (*core.MessageRecord, error) {
	var messageID string
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(threadLastBotBuck).Get(int64Key(threadID))
		if raw != nil {
			messageID = string(raw)
		}
		return nil
	})
	if err != nil || messageID == "" {
		return nil, err
	}
	return s.GetMessage(ctx, messageID)
}

func (s *BoltStore) ClearLastBotMessage(_ context.Context, threadID int64, messageID string) error {
	if threadID == 0 {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(threadLastBotBuck)
		if messageID != "" {
			current := bucket.Get(int64Key(threadID))
			if string(current) != messageID {
				return nil
			}
		}
		return bucket.Delete(int64Key(threadID))
	})
}

func putJSON(bucket *bolt.Bucket, key []byte, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return bucket.Put(key, data)
}

func getJSON[T any](bucket *bolt.Bucket, key []byte, target **T) error {
	raw := bucket.Get(key)
	if raw == nil {
		*target = nil
		return nil
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*target = &value
	return nil
}

func int64Key(v int64) []byte {
	return []byte(fmt.Sprintf("%020d", v))
}

func threadPrefix(threadID int64) string {
	return fmt.Sprintf("%020d|", threadID)
}

func messageIndexKey(threadID, timestampMs int64, messageID string) []byte {
	if timestampMs < 0 {
		timestampMs = 0
	}
	return []byte(fmt.Sprintf("%020d|%020d|%s", threadID, timestampMs, messageID))
}
