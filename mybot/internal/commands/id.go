package commands

import (
	"context"
	"fmt"

	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

type IDCommand struct{}

func (c *IDCommand) Run(ctx *Context) error {
	text := fmt.Sprintf(
		"🔍 Info\nThread ID: %d\nSender ID: %d\nMessage ID: %s",
		ctx.Message.ThreadKey,
		ctx.Message.SenderId,
		ctx.Message.MessageId,
	)

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

func (c *IDCommand) Description() string {
	return "Shows thread and sender ID info"
}
