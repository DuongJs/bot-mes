package registry

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"mybot/internal/core"
)

type cooldownKey struct {
	senderID int64
	command  string
}

type Registry struct {
	commands        map[string]core.CommandHandler
	cooldowns       map[cooldownKey]time.Time
	mu              sync.RWMutex
	DefaultCooldown time.Duration
}

func New() *Registry {
	return &Registry{
		commands:        make(map[string]core.CommandHandler),
		cooldowns:       make(map[cooldownKey]time.Time),
		DefaultCooldown: 3 * time.Second,
	}
}

func (r *Registry) Register(cmd core.CommandHandler) {
	r.commands[strings.ToLower(cmd.Name())] = cmd
}

func (r *Registry) CheckCooldown(senderID int64, command string) (remaining time.Duration, inCooldown bool) {
	key := cooldownKey{senderID: senderID, command: strings.ToLower(command)}
	r.mu.RLock()
	expiry, exists := r.cooldowns[key]
	r.mu.RUnlock()
	if exists {
		remaining = time.Until(expiry)
		if remaining > 0 {
			return remaining, true
		}
	}
	return 0, false
}

func (r *Registry) SetCooldown(senderID int64, command string) {
	key := cooldownKey{senderID: senderID, command: strings.ToLower(command)}
	r.mu.Lock()
	r.cooldowns[key] = time.Now().Add(r.DefaultCooldown)
	r.mu.Unlock()
}

func (r *Registry) Execute(name string, ctx *core.CommandContext) error {
	cmd, ok := r.commands[strings.ToLower(name)]
	if !ok {
		return fmt.Errorf("không tìm thấy lệnh: %s", name)
	}

	if remaining, inCooldown := r.CheckCooldown(ctx.SenderID, name); inCooldown {
		return fmt.Errorf("vui lòng chờ %.1f giây", remaining.Seconds())
	}

	err := cmd.Execute(ctx)
	if err == nil {
		r.SetCooldown(ctx.SenderID, name)
	}
	return err
}

func (r *Registry) CleanCooldowns() {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for k, expiry := range r.cooldowns {
		if now.After(expiry) {
			delete(r.cooldowns, k)
		}
	}
}

func (r *Registry) List() map[string]string {
	list := make(map[string]string)
	for name, cmd := range r.commands {
		list[name] = cmd.Description()
	}
	return list
}
