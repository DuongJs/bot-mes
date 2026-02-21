package media

import (
	"fmt"
	"strings"

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
	return "Tải media từ Facebook, TikTok, Douyin, Instagram"
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

	// 2. Download all in parallel
	type downloadResult struct {
		index    int
		data     []byte
		mime     string
		filename string
		err      error
	}

	results := make(chan downloadResult, len(medias))
	for i, m := range medias {
		go func(idx int, item media.MediaItem) {
			data, mime, err := c.Service.Download(ctx.Ctx, item.URL)
			if err != nil {
				results <- downloadResult{index: idx, err: err}
				return
			}
			results <- downloadResult{
				index:    idx,
				data:     data,
				mime:     mime,
				filename: media.FilenameFromMIME(mime),
			}
		}(i, m)
	}

	// 3. Collect downloads
	attachments := make([]core.MediaAttachment, 0, len(medias))
	for range medias {
		r := <-results
		if r.err != nil {
			ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("Tải xuống #%d thất bại: %v", r.index+1, r.err))
			continue
		}
		attachments = append(attachments, core.MediaAttachment{
			Data:     r.data,
			Filename: r.filename,
			MimeType: r.mime,
		})
	}

	if len(attachments) == 0 {
		return fmt.Errorf("tất cả media đều thất bại")
	}

	// 4. Send all as one message
	if err := ctx.Sender.SendMultiMedia(ctx.Ctx, ctx.ThreadID, attachments); err != nil {
		ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("Gửi media thất bại: %v", err))
	}

	return nil
}
