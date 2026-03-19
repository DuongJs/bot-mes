package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"

	"github.com/rs/zerolog"

	"mybot/internal/app"
	"mybot/internal/config"
)

func initLogger() zerolog.Logger {
	if os.Getenv("LOG_FORMAT") == "json" {
		return zerolog.New(os.Stdout).With().Timestamp().Logger()
	}
	consoleW := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}
	return zerolog.New(consoleW).With().Timestamp().Logger()
}

func main() {
	configPath := "config.json"
	flag.StringVar(&configPath, "config", configPath, "path to config file")
	flag.Parse()

	log := initLogger()

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	// Apply memory tuning from config.
	gcPercent := cfg.Performance.GCPercent
	if gcPercent > 0 && os.Getenv("GOGC") == "" {
		debug.SetGCPercent(gcPercent)
		log.Info().Int("gc_percent", gcPercent).Msg("GOGC overridden from config")
	}
	if memLimitMB := cfg.Performance.MemoryLimitMB; memLimitMB > 0 && os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(int64(memLimitMB) * 1024 * 1024)
		log.Info().Int("memory_limit_mb", memLimitMB).Msg("GOMEMLIMIT overridden from config")
	}
	runtime.SetMutexProfileFraction(0)

	bot, err := app.New(cfg, configPath, log)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize bot")
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		cancel()
	}()

	bot.Run(ctx)
}
