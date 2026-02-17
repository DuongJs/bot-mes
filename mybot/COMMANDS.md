# Adding New Commands

To add a new command to the bot, follow these steps:

1.  **Create a Command File**: Create a new `.go` file in the `internal/commands/` directory (e.g., `mycommand.go`).
2.  **Define Structure**: Define a struct for your command.
3.  **Implement Interface**: Implement the `Command` interface methods:
    - `Run(ctx *Context) error`: Contains the command logic.
    - `Description() string`: Returns a brief description for the help command.
4.  **Register Command**: Register your new command in `cmd/bot/main.go` using `cmds.Register("command_name", &commands.MyCommand{})`.

## Example Command

```go
package commands

import (
	"context"

	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

type MyCommand struct{}

func (c *MyCommand) Run(ctx *Context) error {
	task := &socket.SendMessageTask{
		ThreadId:  ctx.Message.ThreadKey,
		Text:      "Hello from MyCommand!",
		Source:    table.MESSENGER_INBOX_IN_THREAD,
		SendType:  table.TEXT,
		SyncGroup: 1,
	}
	_, err := ctx.Client.ExecuteTask(context.Background(), task)
	return err
}

func (c *MyCommand) Description() string {
	return "An example command"
}
```

## Context Objects

The `Context` struct passed to `Run` provides:
- **Client**: The `*messagix.Client` instance for interacting with the API (e.g., `ExecuteTask`).
- **Message**: The incoming message (`*table.LSUpsertMessage`) that triggered the command.
- **Args**: A slice of strings containing command arguments (excluding the command name).
