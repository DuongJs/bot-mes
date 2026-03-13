package scripting

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
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

// pkgDeclRe matches "package main" at the start of a Go source file.
var pkgDeclRe = regexp.MustCompile(`(?m)^package\s+main\b`)

// rewritePackage replaces "package main" with a unique package name so
// multiple scripts can coexist in a single Yaegi interpreter.
func rewritePackage(src, pkgName string) string {
	return pkgDeclRe.ReplaceAllString(src, "package "+pkgName)
}

// sanitizePkgName converts a directory name into a valid Go identifier
// for use as a package name (e.g. "my-mod" → "mod_my_mod").
func sanitizePkgName(dirName string) string {
	s := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return '_'
	}, dirName)
	return "mod_" + s
}

// LoadModules scans modulesDir for subdirectories containing a command.go file.
// All script modules share a SINGLE Yaegi interpreter to minimise memory usage
// (stdlib.Symbols is loaded only once). Each script's "package main" is
// rewritten to a unique package name to avoid symbol collisions.
// Directories listed in skip are ignored (compiled modules).
func LoadModules(modulesDir string, skip map[string]bool) ([]*ScriptCommand, []error) {
	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		return nil, nil
	}

	// Collect scripts to load.
	type scriptEntry struct {
		dirName string
		path    string
	}
	var scripts []scriptEntry
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
		scripts = append(scripts, scriptEntry{dirName: name, path: cmdFile})
	}
	if len(scripts) == 0 {
		return nil, nil
	}

	// Create ONE shared interpreter for all script modules.
	shared := interp.New(interp.Options{})
	shared.Use(stdlib.Symbols)

	var cmds []*ScriptCommand
	var errs []error

	for _, s := range scripts {
		cmd, err := loadScriptInto(shared, s.path, s.dirName)
		if err != nil {
			errs = append(errs, fmt.Errorf("script module %q: %w", s.dirName, err))
			continue
		}
		cmds = append(cmds, cmd)
	}
	return cmds, errs
}

// loadScriptInto loads a single script into the shared interpreter.
// It rewrites "package main" to a unique package so symbols don't collide.
func loadScriptInto(shared *interp.Interpreter, path, dirName string) (*ScriptCommand, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	pkgName := sanitizePkgName(dirName)
	src := rewritePackage(string(raw), pkgName)

	_, err = shared.Eval(src)
	if err != nil {
		return nil, fmt.Errorf("eval: %w", err)
	}

	// Extract Name() — call it once at load time.
	name, err := evalStringCall(shared, pkgName+".Name()")
	if err != nil {
		return nil, fmt.Errorf("Name(): %w", err)
	}

	// Extract Description() — call it once at load time.
	desc, err := evalStringCall(shared, pkgName+".Description()")
	if err != nil {
		return nil, fmt.Errorf("Description(): %w", err)
	}

	// Extract Execute function value — keep for runtime calls.
	execVal, err := shared.Eval(pkgName + ".Execute")
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
