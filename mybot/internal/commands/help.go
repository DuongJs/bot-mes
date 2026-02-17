package commands

import (
	"fmt"
	"strings"

	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

type HelpCommand struct {
	Registry *Registry
}

func (c *HelpCommand) Run(ctx *Context) error {
	var sb strings.Builder
	sb.WriteString("Available commands:\n")
	for name, desc := range c.Registry.List() {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", name, desc))
	}
	task := &socket.SendMessageTask{
		ThreadId:  ctx.Message.ThreadKey,
		Text:      sb.String(),
		Source:    table.MESSENGER_INBOX_IN_THREAD,
		SendType:  table.TEXT,
		SyncGroup: 1,
		Otid:      methods.GenerateEpochID(),
	}
	_, err := ctx.Client.ExecuteTask(ctx.Ctx, task)
	return err
}

func (c *HelpCommand) Description() string {
	return "Lists all available commands"
}
