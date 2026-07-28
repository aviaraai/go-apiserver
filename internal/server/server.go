package server

import (
	"fmt"
	"net/http"
	"time"

	"go-api-server/internal/config"
	"go-api-server/internal/database"
)

type Server struct {
	cfg *config.Config
	db  database.Service
}

func NewServer(cfg *config.Config, db database.Service) (*http.Server, error) {
	s := &Server{cfg: cfg, db: db}

	handler, err := s.RegisterRoutes()
	if err != nil {
		return nil, fmt.Errorf("server routes registration error: %w", err)
	}

	// Declare Server config
	return &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      handler,
		IdleTimeout:  time.Minute,
		ReadTimeout:  2 * time.Minute,
		WriteTimeout: 2 * time.Minute,
	}, nil
}
