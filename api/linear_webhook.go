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
	"log/slog"
	"net/http"

	"github.com/workdock-dev/engine/domain/types"
)

func (s *Server) handleLinearWebhook(w http.ResponseWriter, r *http.Request) {
	if err := s.app.WebhookService.On(
		r.Context(),
		types.PlatformProvider_Linear,
		types.WebhookRequest{
			Headers:    r.Header,
			RemoteAddr: r.RemoteAddr,
			Body:       r.Body,
		},
	); err != nil {
		slog.Error("linear webhook rejected", "err", err.Error())
		w.WriteHeader(s.domainErrToStatusCode(err))
		return
	}

	w.WriteHeader(http.StatusAccepted)
}
