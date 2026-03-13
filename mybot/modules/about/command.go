package main

import "fmt"

func Name() string {
	return "about"
}

func Description() string {
	return "Thông tin về bot"
}

func Execute(ctx map[string]interface{}) string {
	return "🤖 MyBot v2.0 - Bot Messenger mô-đun"
}

// ── Lưu ý: module "info" gốc có 3 lệnh (about, id, status).
// Trong script mode mỗi lệnh cần 1 thư mục riêng.
// Xem thêm: modules/id/ và modules/status/
func init() { _ = fmt.Sprintf }
