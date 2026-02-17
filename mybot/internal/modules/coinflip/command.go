package coinflip

import (
	"math/rand/v2"

	"mybot/internal/core"
)

type Command struct{}

func (c *Command) Name() string {
	return "coinflip"
}

func (c *Command) Description() string {
	return "Tung đồng xu (Sấp/Ngửa)"
}

func (c *Command) Execute(ctx *core.CommandContext) error {
	result := "🪙 Ngửa"
	if rand.IntN(2) == 0 {
		result = "🪙 Sấp"
	}
	return ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, result)
}
