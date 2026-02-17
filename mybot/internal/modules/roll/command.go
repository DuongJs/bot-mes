package roll

import (
	"fmt"
	"math/rand/v2"
	"strconv"

	"mybot/internal/core"
)

type Command struct{}

func (c *Command) Name() string {
	return "roll"
}

func (c *Command) Description() string {
	return "Tung xúc xắc (mặc định 1-6, hoặc !roll <số>)"
}

func (c *Command) Execute(ctx *core.CommandContext) error {
	max := 6
	if len(ctx.Args) > 0 {
		if n, err := strconv.Atoi(ctx.Args[0]); err == nil && n > 1 {
			max = n
		}
	}
	result := rand.IntN(max) + 1
	return ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, fmt.Sprintf("🎲 Kết quả: %d (1-%d)", result, max))
}
