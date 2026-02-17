package commands

import (
	"fmt"
	"runtime"

	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

const BotVersion = "1.0.0"

type AboutCommand struct{}

func (c *AboutCommand) Run(ctx *Context) error {
	text := fmt.Sprintf(
		"🤖 Bot Info\n"+
			"Version: %s\n"+
			"Go: %s\n"+
			"OS/Arch: %s/%s\n"+
			"Commands: ping, help, media, uptime, about, status, id",
		BotVersion,
		runtime.Version(),
		runtime.GOOS,
		runtime.GOARCH,
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

func (c *AboutCommand) Description() string {
	return "Shows bot information"
}
