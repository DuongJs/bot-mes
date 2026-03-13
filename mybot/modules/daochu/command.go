package main

import (
	"math/rand"
	"strings"
)

func Name() string {
	return "daochu"
}

func Description() string {
	return "Đảo ngẫu nhiên các chữ trong câu"
}

func Execute(ctx map[string]interface{}) string {
	args, _ := ctx["args"].([]string)
	if len(args) == 0 {
		return "Cách dùng: !daochu <câu cần đảo>"
	}

	words := make([]string, len(args))
	copy(words, args)
	rand.Shuffle(len(words), func(i, j int) {
		words[i], words[j] = words[j], words[i]
	})
	return "🔀 " + strings.Join(words, " ")
}
