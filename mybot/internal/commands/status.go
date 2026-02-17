package commands

import (
	"fmt"
	"runtime"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

type StatusCommand struct{}

func (c *StatusCommand) Run(ctx *Context) error {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	d := time.Since(ctx.StartTime)
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	secs := int(d.Seconds()) % 60

	var uptime string
	if days > 0 {
		uptime = fmt.Sprintf("%dd %dh %dm %ds", days, hours, mins, secs)
	} else if hours > 0 {
		uptime = fmt.Sprintf("%dh %dm %ds", hours, mins, secs)
	} else if mins > 0 {
		uptime = fmt.Sprintf("%dm %ds", mins, secs)
	} else {
		uptime = fmt.Sprintf("%ds", secs)
	}

	text := fmt.Sprintf(
		"📊 Bot Status\n"+
			"⏱ Uptime: %s\n"+
			"💾 RAM: %.2f MB\n"+
			"📦 Alloc: %.2f MB\n"+
			"🔄 GC Cycles: %d\n"+
			"🧵 Goroutines: %d\n"+
			"💻 OS/Arch: %s/%s\n"+
			"🔧 Go: %s",
		uptime,
		float64(m.Sys)/(1024*1024),
		float64(m.Alloc)/(1024*1024),
		m.NumGC,
		runtime.NumGoroutine(),
		runtime.GOOS,
		runtime.GOARCH,
		runtime.Version(),
	)

	task := &socket.SendMessageTask{
		ThreadId:  ctx.Message.ThreadKey,
		Text:      text,
		Source:    table.MESSENGER_INBOX_IN_THREAD,
		SendType:  table.TEXT,
		SyncGroup: 1,
		Otid:      methods.GenerateEpochID(),
	}
	_, err := ctx.Client.ExecuteTask(ctx.Ctx, task)
	return err
}

func (c *StatusCommand) Description() string {
	return "Shows bot status: RAM, goroutines, uptime"
}
