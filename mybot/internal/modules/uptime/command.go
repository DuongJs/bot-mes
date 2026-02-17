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
	return "Shows how long the bot has been running"
}

func (c *Command) Execute(ctx *core.CommandContext) error {
	duration := time.Since(ctx.StartTime).Truncate(time.Second)
	return ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("Uptime: %s", duration))
}
