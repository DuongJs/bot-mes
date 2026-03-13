package scripting

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"

	"mybot/internal/core"
)

// ScriptContext is a simplified map passed to script Execute() functions.
type ScriptContext map[string]any

// CommandLister returns a name→description map of all registered commands.
type CommandLister func() map[string]string

// ScriptCommand wraps a Yaegi-interpreted module as a core.CommandHandler.
type ScriptCommand struct {
	name         string
	desc         string
	execFn       reflect.Value
	listCommands CommandLister
}

func (s *ScriptCommand) Name() string        { return s.name }
func (s *ScriptCommand) Description() string { return s.desc }

// SetCommandLister injects a function providing the full command list at runtime.
func (s *ScriptCommand) SetCommandLister(fn CommandLister) {
	s.listCommands = fn
}

func (s *ScriptCommand) Execute(ctx *core.CommandContext) error {
	sctx := ScriptContext{
		"thread_id":  ctx.ThreadID,
		"sender_id":  ctx.SenderID,
		"message_id": ctx.IncomingMessageID,
		"args":       ctx.Args,
		"raw_text":   ctx.RawText,
		"start_time": ctx.StartTime.Format(time.RFC3339),
		"uptime_sec": int64(time.Since(ctx.StartTime).Seconds()),
	}
	if s.listCommands != nil {
		sctx["commands"] = s.listCommands()
	}

	results := s.execFn.Call([]reflect.Value{reflect.ValueOf(sctx)})
	if len(results) > 0 {
		text := results[0].String()
		if text != "" {
			return ctx.Sender.SendMessage(ctx.Ctx, ctx.ThreadID, text)
		}
	}
	return nil
}

// LoadModules scans modulesDir for subdirectories containing a command.go file.
// Each script module is loaded via Yaegi (Go interpreter) at runtime — no
// recompilation required. Directories listed in skip are ignored (they are
// compiled modules already registered by the binary).
func LoadModules(modulesDir string, skip map[string]bool) ([]*ScriptCommand, []error) {
	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		return nil, nil
	}

	var cmds []*ScriptCommand
	var errs []error

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if skip[name] {
			continue
		}

		cmdFile := filepath.Join(modulesDir, name, "command.go")
		if _, err := os.Stat(cmdFile); err != nil {
			continue
		}

		cmd, err := loadScript(cmdFile, name)
		if err != nil {
			errs = append(errs, fmt.Errorf("script module %q: %w", name, err))
			continue
		}
		cmds = append(cmds, cmd)
	}
	return cmds, errs
}

func loadScript(path, dirName string) (*ScriptCommand, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	i := interp.New(interp.Options{})
	i.Use(stdlib.Symbols)

	_, err = i.Eval(string(src))
	if err != nil {
		return nil, fmt.Errorf("eval: %w", err)
	}

	// Extract Name() — call it once at load time.
	name, err := evalStringCall(i, "main.Name()")
	if err != nil {
		return nil, fmt.Errorf("Name(): %w", err)
	}

	// Extract Description() — call it once at load time.
	desc, err := evalStringCall(i, "main.Description()")
	if err != nil {
		return nil, fmt.Errorf("Description(): %w", err)
	}

	// Extract Execute function value — keep for runtime calls.
	// Signature: Execute(ctx map[string]interface{}) string
	execVal, err := i.Eval("main.Execute")
	if err != nil {
		return nil, fmt.Errorf("missing Execute(ctx map[string]interface{}) string: %w", err)
	}
	if execVal.Kind() != reflect.Func {
		return nil, fmt.Errorf("Execute is not a function")
	}

	return &ScriptCommand{
		name:   name,
		desc:   desc,
		execFn: execVal,
	}, nil
}

func evalStringCall(i *interp.Interpreter, expr string) (string, error) {
	v, err := i.Eval(expr)
	if err != nil {
		return "", err
	}
	if v.Kind() == reflect.String {
		return v.String(), nil
	}
	return fmt.Sprint(v.Interface()), nil
}
