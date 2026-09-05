// Copyright 2026 Jaziel Guerrero
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"
)

type Server struct {
	address string
	mux     *http.ServeMux
	srv     *http.Server
}

// New initializes the application's HTTP server and registers all
// public endpoints.
//
//   - Configures the HTTP routes exposed by the application.
//   - Associates each endpoint with the corresponding business workflow.
//   - Prepares the server to receive OAuth callbacks and webhook events from
//     external services.
//
// The server is fully configured after initialization but does not begin
// accepting requests until Run is invoked.
func New(address string) (*Server, error) {
	s := &Server{
		address: address,
	}

	mux := http.NewServeMux()
	// mux.HandleFunc("GET /linear/oauth/authorize", s.handleLinearOauthAuthorize)
	// mux.HandleFunc("GET /linear/oauth/callback", s.handleLinearOauthCallback)
	// mux.HandleFunc("POST /linear/webhook", s.handleLinearWebhook)
	// mux.HandleFunc("POST /github/webhook", s.handleGitHubWebhook)
	s.mux = mux

	slog.Debug("[http-server] created")
	return s, nil
}

func (s *Server) Mux() *http.ServeMux {
	return s.mux
}

// Run starts the HTTP server and blocks until shutdown.
//
// - Listens for HTTP requests on the configured network address.
// - Shuts down gracefully when the context is cancelled.
// - Returns when the server has stopped accepting connections.
//
// The process terminates if the server fails to start or encounters an
// unrecoverable runtime error.
func (s *Server) Run(ctx context.Context) {
	s.srv = &http.Server{Addr: s.address, Handler: s.mux}
	slog.Info("[http-server] started", "address", s.address)

	go func() {
		<-ctx.Done()
		slog.Info("[http-server] shutdown")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := s.srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("[http-server] shutdown failed", "err", err)
		}
	}()

	if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("[http-server] failed running", "err", err)
		os.Exit(1)
	}
}
