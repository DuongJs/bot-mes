// Script module: hello
// Drop this file into modules/hello/command.go and restart the bot.
// No recompilation needed!
package main

import "strings"

func Name() string {
	return "hello"
}

func Description() string {
	return "Gửi lời chào"
}

// Execute receives a context map with keys:
//   thread_id, sender_id, message_id, args, raw_text, start_time, uptime_sec
func Execute(ctx map[string]interface{}) string {
	args, _ := ctx["args"].([]string)
	if len(args) > 0 {
		return "Xin chào, " + strings.Join(args, " ") + "! 👋"
	}
	return "Xin chào! 👋"
}
