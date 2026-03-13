package main

import "strings"

func Name() string {
	return "say"
}

func Description() string {
	return "Lặp lại tin nhắn của bạn"
}

func Execute(ctx map[string]interface{}) string {
	args, _ := ctx["args"].([]string)
	if len(args) == 0 {
		return "Cách dùng: !say <tin nhắn>"
	}
	return "🗣 " + strings.Join(args, " ")
}
