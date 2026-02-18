package dashboard

import (
	"embed"
	"encoding/json"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"mybot/internal/config"
)

//go:embed index.html
var content embed.FS

// CommandLister provides a list of registered commands and their descriptions.
type CommandLister interface {
	List() map[string]string
}

// maxRequestBodySize is the maximum allowed request body size (1 MB).
const maxRequestBodySize = 1 << 20

// Server serves the dashboard UI and API endpoints.
type Server struct {
	Config    *config.Config
	StartTime time.Time
	Restart   func()
	Commands  CommandLister
	Logs      *LogBuffer

	// Live stats updated by the bot.
	connected    atomic.Bool
	messageCount atomic.Int64
}

// New creates a new dashboard server.
func New(cfg *config.Config, restartFunc func()) *Server {
	return &Server{
		Config:    cfg,
		StartTime: time.Now(),
		Restart:   restartFunc,
		Logs:      NewLogBuffer(500),
	}
}

// SetConnected updates the bot connection status shown on the dashboard.
func (s *Server) SetConnected(v bool) { s.connected.Store(v) }

// IncrementMessages increments the processed message counter.
func (s *Server) IncrementMessages() { s.messageCount.Add(1) }

// Start starts the dashboard HTTP server on the given port.
func (s *Server) Start(port string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/restart", s.handleRestart)
	mux.HandleFunc("/api/commands", s.handleCommands)
	mux.HandleFunc("/api/modules", s.handleModules)
	mux.HandleFunc("/api/logs", s.handleLogs)

	go func() {
		srv := &http.Server{
			Addr:         ":" + port,
			Handler:      mux,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  60 * time.Second,
		}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Dashboard server error: %v", err)
		}
	}()
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data, err := content.ReadFile("index.html")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	activeModules := 0
	for _, v := range s.Config.Modules {
		if v {
			activeModules++
		}
	}

	cmdCount := 0
	if s.Commands != nil {
		cmdCount = len(s.Commands.List())
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"uptime":         time.Since(s.StartTime).String(),
		"connected":      s.connected.Load(),
		"message_count":  s.messageCount.Load(),
		"command_count":  cmdCount,
		"active_modules": activeModules,
		"prefix":         s.Config.CommandPrefix,
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
		var newCfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.Config.Update(&newCfg)
		if err := s.Config.Save("config.json"); err != nil {
			http.Error(w, "Failed to save config", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.Config)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.Restart != nil {
		go s.Restart()
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if s.Commands != nil {
		json.NewEncoder(w).Encode(s.Commands.List())
	} else {
		json.NewEncoder(w).Encode(map[string]string{})
	}
}

func (s *Server) handleModules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.Config.Modules)
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
		var modules map[string]bool
		if err := json.NewDecoder(r.Body).Decode(&modules); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.Config.UpdateModules(modules)
		if err := s.Config.Save("config.json"); err != nil {
			http.Error(w, "Failed to save config", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if s.Logs != nil {
		json.NewEncoder(w).Encode(s.Logs.Entries())
	} else {
		json.NewEncoder(w).Encode([]LogEntry{})
	}
}
