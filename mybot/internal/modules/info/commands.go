package info

import (
	"fmt"
	"mybot/internal/core"
)

type AboutCommand struct{}

func (c *AboutCommand) Name() string { return "about" }
func (c *AboutCommand) Description() string { return "Thông tin về bot" }
func (c *AboutCommand) Execute(ctx *core.CommandContext) error {
	return ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, "🤖 MyBot v2.0 - Bot Messenger mô-đun")
}

type IDCommand struct{}

func (c *IDCommand) Name() string { return "id" }
func (c *IDCommand) Description() string { return "Hiển thị thông tin ID" }
func (c *IDCommand) Execute(ctx *core.CommandContext) error {
	return ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("👤 ID người dùng: %d\n💬 ID cuộc trò chuyện: %d", ctx.SenderID, ctx.ThreadID))
}

type StatusCommand struct{}

func (c *StatusCommand) Name() string { return "status" }
func (c *StatusCommand) Description() string { return "Kiểm tra trạng thái hệ thống" }
func (c *StatusCommand) Execute(ctx *core.CommandContext) error {
	return ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, "✅ Tất cả hệ thống hoạt động bình thường.")
}
