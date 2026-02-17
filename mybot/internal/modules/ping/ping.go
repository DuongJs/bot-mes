package ping

import (
	"mybot/internal/core"
)

type Command struct{}

func (c *Command) Name() string {
	return "ping"
}

func (c *Command) Description() string {
	return "Replies with Pong!"
}

func (c *Command) Execute(ctx *core.CommandContext) error {
	return ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, "Pong!")
}
