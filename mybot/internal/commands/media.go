package commands

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"mybot/internal/media"

	"go.mau.fi/mautrix-meta/pkg/messagix"
	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

type MediaCommand struct{}

func (c *MediaCommand) Run(ctx *Context) error {
	if len(ctx.Args) == 0 {
		return fmt.Errorf("usage: !media <url>")
	}

	url := ctx.Args[0]
	// Basic URL validation
	if !strings.HasPrefix(url, "http") {
		return fmt.Errorf("invalid url")
	}

	medias, err := media.GetMedia(context.Background(), url)
	if err != nil {
		// Send error message
		c.sendMessage(ctx, fmt.Sprintf("Error: %v", err))
		return err
	}

	if len(medias) == 0 {
		c.sendMessage(ctx, "No media found")
		return nil
	}

	// Process media items concurrently
	var wg sync.WaitGroup
	results := make(chan int64, len(medias))

	for i, m := range medias {
		wg.Add(1)
		go func(idx int, item media.MediaItem) {
			defer wg.Done()

			data, mime, err := media.DownloadMedia(context.Background(), item.URL)
			if err != nil {
				c.sendMessage(ctx, fmt.Sprintf("Failed to download media #%d: %v", idx+1, err))
				return
			}

			uploadResp, err := ctx.Client.SendMercuryUploadRequest(context.Background(), ctx.Message.ThreadKey, &messagix.MercuryUploadMedia{
				Filename:  "media",
				MimeType:  mime,
				MediaData: data,
			})
			if err != nil {
				c.sendMessage(ctx, fmt.Sprintf("Failed to upload media #%d: %v", idx+1, err))
				return
			}

			var realFBID int64
			if uploadResp.Payload.RealMetadata != nil {
				realFBID = uploadResp.Payload.RealMetadata.GetFbId()
			}
			if realFBID != 0 {
				results <- realFBID
			}
		}(i, m)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for fbID := range results {
		task := &socket.SendMessageTask{
			ThreadId:        ctx.Message.ThreadKey,
			AttachmentFBIds: []int64{fbID},
			Source:          table.MESSENGER_INBOX_IN_THREAD,
			SendType:        table.MEDIA,
			SyncGroup:       1,
			Otid:            methods.GenerateEpochID(),
		}
		ctx.Client.ExecuteTask(context.Background(), task)
	}

	return nil
}

func (c *MediaCommand) sendMessage(ctx *Context, text string) {
	task := &socket.SendMessageTask{
		ThreadId:  ctx.Message.ThreadKey,
		Text:      text,
		Source:    table.MESSENGER_INBOX_IN_THREAD,
		SendType:  table.TEXT,
		SyncGroup: 1,
		Otid:      methods.GenerateEpochID(),
	}
	ctx.Client.ExecuteTask(context.Background(), task)
}

func (c *MediaCommand) Description() string {
	return "Downloads media from Facebook, TikTok, Instagram"
}
