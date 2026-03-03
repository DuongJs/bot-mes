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

	// 2. Download all to temp files via the global download pool.
	//    The pool limits system-wide concurrency (default 8), so many users
	//    can download simultaneously without OOM, while one user with 10
	//    images gets all available slots for maximum speed.
	results := c.Service.DownloadBatch(ctx.Ctx, medias)

	// Ensure all temp files are cleaned up when we're done.
	defer func() {
		for i := range results {
			if results[i].File != nil {
				results[i].File.Cleanup()
			}
		}
		debug.FreeOSMemory()
	}()

	// 3. Collect successful downloads.
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

	if len(attachments) == 0 {
		return fmt.Errorf("tất cả media đều thất bại")
	}

	// 4. Send — upload reads data one file at a time from disk, releases immediately.
	if err := ctx.Sender.SendMultiMedia(ctx.Ctx, ctx.ThreadID, attachments); err != nil {
		ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("Gửi media thất bại: %v", err))
	}

	return nil
}
