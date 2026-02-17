package commands

import (
	"fmt"
	"strings"

	"go.mau.fi/mautrix-meta/pkg/messagix"
)

type WrappedMessage struct {
	ThreadKey int64
	Text      string
	SenderId  int64
	MessageId string
}

type Context struct {
	Client  *messagix.Client
	Message *WrappedMessage
	Args    []string
}

type Command interface {
	Run(ctx *Context) error
	Description() string
}

type Registry struct {
	commands map[string]Command
}

func NewRegistry() *Registry {
	return &Registry{
		commands: make(map[string]Command),
	}
}

func (r *Registry) Register(name string, cmd Command) {
	r.commands[strings.ToLower(name)] = cmd
}

func (r *Registry) Get(name string) (Command, bool) {
	cmd, ok := r.commands[strings.ToLower(name)]
	return cmd, ok
}

func (r *Registry) Execute(name string, ctx *Context) error {
	if cmd, ok := r.Get(name); ok {
		return cmd.Run(ctx)
	}
	return fmt.Errorf("command not found: %s", name)
}

func (r *Registry) List() map[string]string {
	list := make(map[string]string)
	for name, cmd := range r.commands {
		list[name] = cmd.Description()
	}
	return list
}
