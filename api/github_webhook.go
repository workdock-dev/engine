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
	"errors"
	"net/http"

	"github.com/workdock-dev/engine/domain/types"
	"github.com/workdock-dev/engine/pipelines/github"
	"github.com/workdock-dev/engine/pipelines/runners"
)

func (s *Server) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if err := runners.NewWebhookRunner(
		github.NewWEventTransformer(),
		github.NewWEventVerifier(),
		github.NewWEventConsumer(),
	).Execute(r); err != nil {
		status := http.StatusInternalServerError

		if errors.Is(err, runners.ErrWBadRequest) {
			status = http.StatusBadRequest
		}

		if errors.Is(err, runners.ErrWUnAuthorized) {
			status = http.StatusUnauthorized
		}

		if errors.Is(err, runners.ErrWForBidden) {
			status = http.StatusForbidden
		}

		w.WriteHeader(status)
		return
	}

	if err := s.app.GetWebhookService().On(r.Context(), types.PlatformProvider_GitHub, types.WebhookRequest{
		Headers:    r.Header,
		RemoteAddr: r.RemoteAddr,
		Body:       r.Body,
	}); err != nil {
		w.WriteHeader(s.domainErrToStatusCode(err))
		return
	}

	w.WriteHeader(http.StatusAccepted)
}
