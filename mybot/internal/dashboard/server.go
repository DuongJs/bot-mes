package dashboard

import (
	"embed"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"mybot/internal/config"
)

//go:embed index.html
var content embed.FS

type Server struct {
	Config    *config.Config
	StartTime time.Time
	Restart   func()
}

func New(cfg *config.Config, restartFunc func()) *Server {
	return &Server{
		Config:    cfg,
		StartTime: time.Now(),
		Restart:   restartFunc,
	}
}

func (s *Server) Start(port string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/restart", s.handleRestart)

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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"uptime": time.Since(s.StartTime).String(),
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		// Limit request body to 1 MB to prevent memory exhaustion
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
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
