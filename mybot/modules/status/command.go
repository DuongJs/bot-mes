package main

import (
	"fmt"
	"runtime"
)

func Name() string {
	return "status"
}

func Description() string {
	return "Kiểm tra trạng thái hệ thống"
}

func Execute(ctx map[string]interface{}) string {
	sec, _ := ctx["uptime_sec"].(int64)
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	return fmt.Sprintf("📊 Bot Status\n"+
		"⏱ Uptime: %dh %dm %ds\n"+
		"💾 RAM: %.2f MB\n"+
		"📦 Alloc: %.2f MB\n"+
		"🔄 GC Cycles: %d\n"+
		"🧵 Goroutines: %d\n"+
		"💻 OS/Arch: %s/%s\n"+
		"🔧 Go: %s",
		h, m, s,
		float64(mem.Sys)/1024/1024,
		float64(mem.Alloc)/1024/1024,
		mem.NumGC,
		runtime.NumGoroutine(),
		runtime.GOOS, runtime.GOARCH,
		runtime.Version(),
	)
}
