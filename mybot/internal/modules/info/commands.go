package info

import (
	"fmt"
	"mybot/internal/core"
)

type AboutCommand struct{}

func (c *AboutCommand) Name() string { return "about" }
func (c *AboutCommand) Description() string { return "About this bot" }
func (c *AboutCommand) Execute(ctx *core.CommandContext) error {
	return ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, "MyBot v2.0 - Refactored Modular Bot")
}

type IDCommand struct{}

func (c *IDCommand) Name() string { return "id" }
func (c *IDCommand) Description() string { return "Shows identifiers" }
func (c *IDCommand) Execute(ctx *core.CommandContext) error {
	return ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("User ID: %d\nThread ID: %d", ctx.SenderID, ctx.ThreadID))
}

type StatusCommand struct{}

func (c *StatusCommand) Name() string { return "status" }
func (c *StatusCommand) Description() string { return "Check system status" }
func (c *StatusCommand) Execute(ctx *core.CommandContext) error {
	return ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, "All systems operational.")
}
