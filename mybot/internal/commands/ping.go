package commands

import (
	"context"

	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

type PingCommand struct{}

func (c *PingCommand) Run(ctx *Context) error {
	task := &socket.SendMessageTask{
		ThreadId:  ctx.Message.ThreadKey,
		Text:      "Pong!",
		Source:    table.MESSENGER_INBOX_IN_THREAD,
		SendType:  table.TEXT,
		SyncGroup: 1,
		Otid:      methods.GenerateEpochID(),
	}
	_, err := ctx.Client.ExecuteTask(context.Background(), task)
	return err
}

func (c *PingCommand) Description() string {
	return "Replies with Pong!"
}
