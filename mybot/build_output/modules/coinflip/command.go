package main

import "math/rand"

func Name() string {
	return "coinflip"
}

func Description() string {
	return "Tung đồng xu (Sấp/Ngửa)"
}

func Execute(ctx map[string]interface{}) string {
	if rand.Intn(2) == 0 {
		return "🪙 Sấp"
	}
	return "🪙 Ngửa"
}
