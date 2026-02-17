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
	return "Downloads media from Facebook, TikTok, Instagram"
}

func (c *Command) Execute(ctx *core.CommandContext) error {
	if len(ctx.Args) == 0 {
		return fmt.Errorf("usage: !media <url>")
	}
	url := ctx.Args[0]

	// Validate URL
	if !strings.HasPrefix(url, "http") {
		return fmt.Errorf("invalid url")
	}

	// 1. Get media items
	medias, err := c.Service.GetMediaItems(ctx.Ctx, url)
	if err != nil {
		ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("Error: %v", err))
		return err
	}
	if len(medias) == 0 {
		ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, "No media found")
		return nil
	}

	ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("Found %d media items, processing...", len(medias)))

	// 2. Download and Send Concurrently
	var wg sync.WaitGroup
	for i, m := range medias {
		wg.Add(1)
		go func(idx int, item media.MediaItem) {
			defer wg.Done()

			// Download
			data, mime, err := c.Service.Download(ctx.Ctx, item.URL)
			if err != nil {
				ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("Failed to download #%d: %v", idx+1, err))
				return
			}

			// Send
			if err := ctx.Sender.SendMedia(ctx.Ctx, ctx.ThreadID, data, "media", mime); err != nil {
				ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("Failed to send #%d: %v", idx+1, err))
			}
		}(i, m)
	}

	wg.Wait()

	return nil
}
