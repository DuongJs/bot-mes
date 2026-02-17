APP_NAME := bot
BUILD_DIR := release
SRC := ./cmd/bot

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X mybot/internal/commands.BotVersion=$(VERSION)

.PHONY: all build build-linux build-windows clean

all: build-linux build-windows

build:
	go build -ldflags "$(LDFLAGS)" -o $(APP_NAME) $(SRC)

build-linux:
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME)-linux-amd64 $(SRC)

build-windows:
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME)-windows-amd64.exe $(SRC)

clean:
	rm -rf $(BUILD_DIR) $(APP_NAME)
