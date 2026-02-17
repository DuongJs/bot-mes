package uptime

import (
	"fmt"
	"time"

	"mybot/internal/core"
)

type Command struct{}

func (c *Command) Name() string {
	return "uptime"
}

func (c *Command) Description() string {
	return "Hiển thị thời gian bot đã hoạt động"
}

func (c *Command) Execute(ctx *core.CommandContext) error {
	duration := time.Since(ctx.StartTime).Truncate(time.Second)
	return ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("⏱ Thời gian hoạt động: %s", duration))
}
