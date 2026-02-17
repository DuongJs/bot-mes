package say

import (
	"fmt"
	"strings"

	"mybot/internal/core"
)

type Command struct{}

func (c *Command) Name() string {
	return "say"
}

func (c *Command) Description() string {
	return "Lặp lại tin nhắn của bạn"
}

func (c *Command) Execute(ctx *core.CommandContext) error {
	if len(ctx.Args) == 0 {
		return fmt.Errorf("cách dùng: !say <tin nhắn>")
	}
	text := strings.Join(ctx.Args, " ")
	return ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, "🗣 "+text)
}
