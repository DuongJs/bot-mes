package media

import (
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"mybot/internal/core"
)

type Command struct {
	Service *Service
	log     zerolog.Logger
}

func NewCommand(service *Service, log zerolog.Logger) *Command {
	return &Command{
		Service: service,
		log:     log,
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

	phaseStart := time.Now()

	// Phase 1: Resolve URL and get media items
	c.log.Info().Str("url", url).Msg("[media] Phase 1: Resolving URL")
	result, err := c.Service.GetMediaItems(ctx.Ctx, url)
	if err != nil {
		ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("Lỗi: %v", err))
		return err
	}
	c.log.Info().
		Int("items", len(result.Items)).
		Dur("duration", time.Since(phaseStart)).
		Msg("[media] Phase 1 complete: URL resolved")

	if len(result.Items) == 0 {
		ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, "Không tìm thấy media")
		return nil
	}

	// Phase 2: Download all media to temp files
	phaseStart = time.Now()
	c.log.Info().Int("count", len(result.Items)).Msg("[media] Phase 2: Downloading media")
	results := c.Service.DownloadBatch(ctx.Ctx, result.Items)

	// Count successful downloads
	successCount := 0
	var totalBytes int64
	for _, r := range results {
		if r.Err == nil && r.File != nil {
			successCount++
			totalBytes += r.File.Size
		}
	}
	c.log.Info().
		Int("success", successCount).
		Int("total", len(results)).
		Int64("total_bytes", totalBytes).
		Dur("duration", time.Since(phaseStart)).
		Msg("[media] Phase 2 complete: Downloads finished")

	// Ensure all temp files are cleaned up when done.
	defer func() {
		for i := range results {
			if results[i].File != nil {
				results[i].File.Cleanup()
			}
		}
		debug.FreeOSMemory()
	}()

	// Phase 3: Collect successful downloads.
	var attachments []core.MediaAttachment
	for _, r := range results {
		if r.Err != nil {
			ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("Tải xuống #%d thất bại: %v", r.Index+1, r.Err))
			continue
		}
		file := r.File
		attachments = append(attachments, core.MediaAttachment{
			FilePath: file.Path,
			FileSize: file.Size,
			Filename: file.Filename,
			MimeType: file.MimeType,
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

	// Phase 5: Upload and send media
	phaseStart = time.Now()
	var uploadBytes int64
	for _, a := range attachments {
		uploadBytes += a.FileSize
	}
	c.log.Info().
		Int("count", len(attachments)).
		Int64("total_bytes", uploadBytes).
		Msg("[media] Phase 5: Uploading media")

	caption := ""
	if result.Message != "" {
		caption = result.Message
	}

	// Always use SendMultiMedia — it leverages file-backed streaming via
	// OpenReader(), avoiding loading the entire file (up to 25 MB) into RAM.
	sendErr := ctx.Sender.SendMultiMedia(ctx.Ctx, ctx.ThreadID, attachments, caption)

	if sendErr != nil {
		c.log.Error().
			Err(sendErr).
			Dur("duration", time.Since(phaseStart)).
			Msg("[media] Phase 5 failed: Upload error")
	} else {
		c.log.Info().
			Dur("duration", time.Since(phaseStart)).
			Msg("[media] Phase 5 complete: Upload successful")
	}

	return sendErr
}
