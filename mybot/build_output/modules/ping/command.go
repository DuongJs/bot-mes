package main

func Name() string {
	return "ping"
}

func Description() string {
	return "Trả lời Pong!"
}

func Execute(ctx map[string]interface{}) string {
	return "Pong!"
}
