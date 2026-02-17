package dashboard

import (
	"embed"
	"encoding/json"
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
	http.HandleFunc("/", s.handleIndex)
	http.HandleFunc("/api/status", s.handleStatus)
	http.HandleFunc("/api/config", s.handleConfig)
	http.HandleFunc("/api/restart", s.handleRestart)

	go func() {
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			panic(err)
		}
	}()
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, _ := content.ReadFile("index.html")
	w.Write(data)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"uptime": time.Since(s.StartTime).String(),
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var newCfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.Config.Update(&newCfg)
		s.Config.Save("config.json")
		return
	}
	json.NewEncoder(w).Encode(s.Config)
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		if s.Restart != nil {
			go s.Restart()
		}
	}
}
