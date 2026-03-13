package media

import (
	"fmt"
	"runtime/debug"
	"strings"

	"mybot/internal/core"
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

	// 1. Get media result (items + optional post text)
	result, err := c.Service.GetMediaItems(ctx.Ctx, url)
	if err != nil {
		ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("Lỗi: %v", err))
		return err
	}
	if len(result.Items) == 0 {
		ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, "Không tìm thấy media")
		return nil
	}

	// 1b. Download all media to temp files via the global download pool.
	results := c.Service.DownloadBatch(ctx.Ctx, result.Items)

	// Ensure all temp files are cleaned up when we're done.
	defer func() {
		for i := range results {
			if results[i].File != nil {
				results[i].File.Cleanup()
			}
		}
		debug.FreeOSMemory()
	}()

	// 2. Collect successful downloads.
	var attachments []core.MediaAttachment
	for _, r := range results {
		if r.Err != nil {
			ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("Tải xuống #%d thất bại: %v", r.Index+1, r.Err))
			continue
		}
		attachments = append(attachments, core.MediaAttachment{
			FilePath: r.File.Path,
			FileSize: r.File.Size,
			Filename: r.File.Filename,
			MimeType: r.File.MimeType,
		})
	}

	if len(attachments) == 0 && result.Message != "" {
		// Chỉ có nội dung, không có media
		ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, result.Message)
		return nil
	}
	if len(attachments) == 0 {
		return fmt.Errorf("tất cả media đều thất bại")
	}

	// 3. Gửi nội dung và media trong cùng 1 tin nhắn nếu có cả hai
	caption := ""
	if result.Message != "" {
		caption = result.Message
	}
	if len(attachments) == 1 {
		// Đọc data nếu cần
		data, err := attachments[0].GetData()
		if err != nil {
			return fmt.Errorf("failed to read media data: %w", err)
		}
		return ctx.Sender.SendMedia(ctx.Ctx, ctx.ThreadID, data, attachments[0].Filename, attachments[0].MimeType, caption)
	}
	// Nhiều media: gửi kèm caption
	return ctx.Sender.SendMultiMedia(ctx.Ctx, ctx.ThreadID, attachments, caption)
}
