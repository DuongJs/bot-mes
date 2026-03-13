package main

import (
	"fmt"
	"sort"
	"strings"
)

func Name() string {
	return "help"
}

func Description() string {
	return "Hiển thị danh sách các lệnh"
}

func Execute(ctx map[string]interface{}) string {
	commands, _ := ctx["commands"].(map[string]string)
	if len(commands) == 0 {
		return "Không có lệnh nào."
	}

	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("📋 Danh sách lệnh (%d):\n", len(names)))
	for _, name := range names {
		b.WriteString(fmt.Sprintf("- %s: %s\n", name, commands[name]))
	}
	return b.String()
}
