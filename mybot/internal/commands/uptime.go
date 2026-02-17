package commands

import (
	"context"
	"fmt"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

type UptimeCommand struct{}

func (c *UptimeCommand) Run(ctx *Context) error {
	d := time.Since(ctx.StartTime)
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	secs := int(d.Seconds()) % 60

	var text string
	if days > 0 {
		text = fmt.Sprintf("⏱ Uptime: %dd %dh %dm %ds", days, hours, mins, secs)
	} else if hours > 0 {
		text = fmt.Sprintf("⏱ Uptime: %dh %dm %ds", hours, mins, secs)
	} else if mins > 0 {
		text = fmt.Sprintf("⏱ Uptime: %dm %ds", mins, secs)
	} else {
		text = fmt.Sprintf("⏱ Uptime: %ds", secs)
	}

	task := &socket.SendMessageTask{
		ThreadId:  ctx.Message.ThreadKey,
		Text:      text,
		Source:    table.MESSENGER_INBOX_IN_THREAD,
		SendType:  table.TEXT,
		SyncGroup: 1,
		Otid:      methods.GenerateEpochID(),
	}
	_, err := ctx.Client.ExecuteTask(context.Background(), task)
	return err
}

func (c *UptimeCommand) Description() string {
	return "Shows how long the bot has been running"
}
