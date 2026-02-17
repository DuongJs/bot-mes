# Adding New Commands

To add a new command to the bot, follow these steps:

1.  **Create a Module Directory**: Create a new directory under `internal/modules/` (e.g., `internal/modules/mycommand/`).
2.  **Create a Command File**: Create a `command.go` file in that directory.
3.  **Define Structure**: Define a struct for your command.
4.  **Implement Interface**: Implement the `core.CommandHandler` interface methods:
    - `Name() string`: Returns the command name used to invoke it (e.g., `"mycommand"`).
    - `Description() string`: Returns a brief description for the help command.
    - `Execute(ctx *core.CommandContext) error`: Contains the command logic.
5.  **Register Command**: Register your new command in `cmd/bot/main.go` using `cmds.Register(&mycommand.Command{})`.
6.  **Enable Module**: Add `"mycommand": true` to the `modules` section in `config.json`.

## Example Command

```go
package mycommand

import (
	"mybot/internal/core"
)

type Command struct{}

func (c *Command) Name() string {
	return "mycommand"
}

func (c *Command) Description() string {
	return "An example command"
}

func (c *Command) Execute(ctx *core.CommandContext) error {
	return ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, "Hello from MyCommand!")
}
```

## Context Objects

The `CommandContext` struct passed to `Execute` provides:
- **Ctx**: The `context.Context` for the request.
- **Sender**: The `MessageSender` interface for sending messages and media (e.g., `SendMessage`, `SendMedia`).
- **ThreadID**: The ID of the conversation thread.
- **SenderID**: The ID of the user who sent the message.
- **Args**: A slice of strings containing command arguments (excluding the command name).
- **RawText**: The original full text of the message.
- **StartTime**: The time the bot started (useful for uptime calculations).
