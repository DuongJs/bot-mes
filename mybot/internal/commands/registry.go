package commands

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix"
)

type WrappedMessage struct {
	ThreadKey int64
	Text      string
	SenderId  int64
	MessageId string
}

type Context struct {
	Ctx       context.Context
	Client    *messagix.Client
	Message   *WrappedMessage
	Args      []string
	StartTime time.Time
}

type Command interface {
	Run(ctx *Context) error
	Description() string
}

// cooldownKey uniquely identifies a user+command pair for rate limiting.
type cooldownKey struct {
	senderID int64
	command  string
}

type Registry struct {
	commands  map[string]Command
	cooldowns map[cooldownKey]time.Time
	mu        sync.RWMutex
	// DefaultCooldown is the minimum interval between command uses per user.
	DefaultCooldown time.Duration
}

func NewRegistry() *Registry {
	return &Registry{
		commands:        make(map[string]Command),
		cooldowns:       make(map[cooldownKey]time.Time),
		DefaultCooldown: 3 * time.Second,
	}
}

func (r *Registry) Register(name string, cmd Command) {
	r.commands[strings.ToLower(name)] = cmd
}

func (r *Registry) Get(name string) (Command, bool) {
	cmd, ok := r.commands[strings.ToLower(name)]
	return cmd, ok
}

// CheckCooldown returns true if the sender is still in cooldown for the given command.
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

// SetCooldown records that a command was used, applying the default cooldown.
func (r *Registry) SetCooldown(senderID int64, command string) {
	key := cooldownKey{senderID: senderID, command: strings.ToLower(command)}
	r.mu.Lock()
	r.cooldowns[key] = time.Now().Add(r.DefaultCooldown)
	r.mu.Unlock()
}

// CleanCooldowns removes expired cooldown entries to free memory.
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

func (r *Registry) Execute(name string, ctx *Context) error {
	cmd, ok := r.Get(name)
	if !ok {
		return fmt.Errorf("command not found: %s", name)
	}

	// Enforce cooldown
	if remaining, inCooldown := r.CheckCooldown(ctx.Message.SenderId, name); inCooldown {
		return fmt.Errorf("cooldown: please wait %.1fs", remaining.Seconds())
	}

	err := cmd.Run(ctx)
	if err == nil {
		r.SetCooldown(ctx.Message.SenderId, name)
	}
	return err
}

func (r *Registry) List() map[string]string {
	list := make(map[string]string)
	for name, cmd := range r.commands {
		list[name] = cmd.Description()
	}
	return list
}
