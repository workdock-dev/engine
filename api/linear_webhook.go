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

package api

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"

	"github.com/jazielguerrero/workdock/domain/types"
)

func (s *Server) handleLinearWebhook(w http.ResponseWriter, r *http.Request) {
	// Buffer the body so it can be read by both the agent session webhook
	// handler and the issue status change handler.
	body, err := io.ReadAll(r.Body)

	if err != nil {
		slog.Error("failed to read linear webhook body", "err", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if err := s.app.WebhookService.On(
		r.Context(),
		types.PlatformProvider_Linear,
		types.WebhookRequest{
			Headers:    r.Header,
			RemoteAddr: r.RemoteAddr,
			Body:       io.NopCloser(bytes.NewReader(body)),
		},
	); err != nil {
		slog.Error("linear webhook rejected", "err", err.Error())
		w.WriteHeader(s.domainErrToStatusCode(err))
		return
	}

	// Also check for issue status change webhooks (e.g., ticket moved to "done").
	// This is best-effort: if the webhook is not an issue status change, it is
	// silently ignored.
	if err := s.app.WebhookService.OnIssueStatusChange(
		r.Context(),
		types.PlatformProvider_Linear,
		types.WebhookRequest{
			Headers:    r.Header,
			RemoteAddr: r.RemoteAddr,
			Body:       io.NopCloser(bytes.NewReader(body)),
		},
	); err != nil {
		// Log the error but don't fail the webhook response since the agent
		// session event was already accepted.
		slog.Error("linear issue status change webhook error", "err", err.Error())
	}

	w.WriteHeader(http.StatusAccepted)
}
