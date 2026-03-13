package main

import "fmt"

func Name() string {
	return "uptime"
}

func Description() string {
	return "Hiển thị thời gian bot đã hoạt động"
}

func Execute(ctx map[string]interface{}) string {
	sec, _ := ctx["uptime_sec"].(int64)
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	return fmt.Sprintf("⏱ Thời gian hoạt động: %dh %dm %ds", h, m, s)
}
