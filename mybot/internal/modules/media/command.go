package media

import (
	"fmt"
	"strings"
	"sync"

	"mybot/internal/core"
	"mybot/internal/media"
)

type Command struct {
	Service *Service
}

func NewCommand(service *Service) *Command {
	return &Command{
		Service: service,
	}
}

func (c *Command) Name() string {
	return "media"
}

func (c *Command) Description() string {
	return "Tải media từ Facebook, TikTok, Instagram"
}

func (c *Command) Execute(ctx *core.CommandContext) error {
	if len(ctx.Args) == 0 {
		return fmt.Errorf("cách dùng: !media <đường dẫn>")
	}
	url := ctx.Args[0]

	// Validate URL
	if !strings.HasPrefix(url, "http") {
		return fmt.Errorf("đường dẫn không hợp lệ")
	}

	// 1. Get media items
	medias, err := c.Service.GetMediaItems(ctx.Ctx, url)
	if err != nil {
		ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("Lỗi: %v", err))
		return err
	}
	if len(medias) == 0 {
		ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, "Không tìm thấy media")
		return nil
	}

	ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("Tìm thấy %d media, đang xử lý...", len(medias)))

	// 2. Download and Send Concurrently
	var wg sync.WaitGroup
	for i, m := range medias {
		wg.Add(1)
		go func(idx int, item media.MediaItem) {
			defer wg.Done()

			// Download
			data, mime, err := c.Service.Download(ctx.Ctx, item.URL)
			if err != nil {
				ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("Tải xuống #%d thất bại: %v", idx+1, err))
				return
			}

			// Send
			if err := ctx.Sender.SendMedia(ctx.Ctx, ctx.ThreadID, data, media.FilenameFromMIME(mime), mime); err != nil {
				ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("Gửi #%d thất bại: %v", idx+1, err))
			}
		}(i, m)
	}

	wg.Wait()

	return nil
}
