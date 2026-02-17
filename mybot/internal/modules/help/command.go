package help

import (
	"fmt"
	"sort"
	"strings"

	"mybot/internal/core"
)

type Lister interface {
	List() map[string]string
}

type Command struct {
	Registry Lister
}

func NewCommand(registry Lister) *Command {
	return &Command{
		Registry: registry,
	}
}

func (c *Command) Name() string {
	return "help"
}

func (c *Command) Description() string {
	return "Hiển thị danh sách các lệnh"
}

func (c *Command) Execute(ctx *core.CommandContext) error {
	list := c.Registry.List()

	names := make([]string, 0, len(list))
	for name := range list {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("📋 Danh sách lệnh:\n")
	for _, name := range names {
		desc := list[name]
		b.WriteString(fmt.Sprintf("- %s: %s\n", name, desc))
	}

	return ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, b.String())
}
