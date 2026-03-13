package main

import "fmt"

func Name() string {
	return "id"
}

func Description() string {
	return "Hiển thị thông tin ID"
}

func Execute(ctx map[string]interface{}) string {
	senderID, _ := ctx["sender_id"].(int64)
	threadID, _ := ctx["thread_id"].(int64)
	return fmt.Sprintf("👤 ID người dùng: %d\n💬 ID cuộc trò chuyện: %d", senderID, threadID)
}
