package main

import (
	"fmt"
	"math/rand"
	"strconv"
)

func Name() string {
	return "roll"
}

func Description() string {
	return "Tung xúc xắc (mặc định 1-6, hoặc !roll <số>)"
}

func Execute(ctx map[string]interface{}) string {
	args, _ := ctx["args"].([]string)
	max := 6
	if len(args) > 0 {
		if n, err := strconv.Atoi(args[0]); err == nil && n > 1 {
			max = n
		}
	}
	result := rand.Intn(max) + 1
	return fmt.Sprintf("🎲 Kết quả: %d (1-%d)", result, max)
}
