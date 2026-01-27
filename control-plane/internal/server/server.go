package server

import (
	"context"
	"net/http"
	"time"
)

type Server struct {
	httpServer *http.Server
}

func New(
	address string,
	handler http.Handler,
	readTimeout time.Duration,
	writeTimeout time.Duration,
) *Server {
	return &Server{
		httpServer: &http.Server{
			Addr:         address,
			Handler:      handler, // router goes here
			ReadTimeout:  readTimeout,
			WriteTimeout: writeTimeout,
		},
	}
}

func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
