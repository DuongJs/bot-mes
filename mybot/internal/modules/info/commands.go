package info

import (
	"fmt"
	"runtime"
	"time"

	"mybot/internal/core"
)

type AboutCommand struct{}

func (c *AboutCommand) Name() string        { return "about" }
func (c *AboutCommand) Description() string { return "Thông tin về bot" }
func (c *AboutCommand) Execute(ctx *core.CommandContext) error {
	return ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, "🤖 MyBot v2.0 - Bot Messenger mô-đun")
}

type IDCommand struct{}

func (c *IDCommand) Name() string        { return "id" }
func (c *IDCommand) Description() string { return "Hiển thị thông tin ID" }
func (c *IDCommand) Execute(ctx *core.CommandContext) error {
	return ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("👤 ID người dùng: %d\n💬 ID cuộc trò chuyện: %d", ctx.SenderID, ctx.ThreadID))
}

type StatusCommand struct{}

func (c *StatusCommand) Name() string        { return "status" }
func (c *StatusCommand) Description() string { return "Kiểm tra trạng thái hệ thống" }
func (c *StatusCommand) Execute(ctx *core.CommandContext) error {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	d := time.Since(ctx.StartTime)
	h := int(d.Hours())
	min := int(d.Minutes()) % 60
	sec := int(d.Seconds()) % 60

	msg := fmt.Sprintf("📊 Bot Status\n"+
		"⏱ Uptime: %dh %dm %ds\n"+
		"💾 RAM: %.2f MB\n"+
		"📦 Alloc: %.2f MB\n"+
		"🔄 GC Cycles: %d\n"+
		"🧵 Goroutines: %d\n"+
		"💻 OS/Arch: %s/%s\n"+
		"🔧 Go: %s",
		h, min, sec,
		float64(m.Sys)/1024/1024,
		float64(m.Alloc)/1024/1024,
		m.NumGC,
		runtime.NumGoroutine(),
		runtime.GOOS, runtime.GOARCH,
		runtime.Version(),
	)

	return ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, msg)
}
