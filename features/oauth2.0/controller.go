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

package oauth20

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/workdock-dev/engine/shared"
)

const (
	HTMLResponse = `<html><head><title>OAuth Success</title></head><body>
<h1>OAuth Authorization Successful!</h1>
<p>Access token received and stored securely: <strong>%s</strong></p>
</body></html>`
)

type Token struct {
	AccessToken  string    `yaml:"access_token"`
	RefreshToken string    `yaml:"refresh_token"`
	ExpiresAt    time.Time `yaml:"expires_at"`
}

type CallbackResult struct {
	EntityId   string
	EntityName string
	Message    string
	Token      Token
}

type OauthHandler interface {
	GetAuthorizationURL() string
	Callback(ctx context.Context, code, err string) (*CallbackResult, error)
}

type controller struct {
	provider      string
	handler       OauthHandler
	secretManager shared.ForSecrets
	eventBus      shared.ForEventBus
}

func New(
	provider string,
	mux *http.ServeMux,
	handler OauthHandler,
	secretManager shared.ForSecrets,
	eventBus shared.ForEventBus,
) {
	c := &controller{
		provider:      provider,
		handler:       handler,
		secretManager: secretManager,
		eventBus:      eventBus,
	}

	mux.HandleFunc(fmt.Sprintf("GET /%s/oauth/authorize", provider), func(w http.ResponseWriter, r *http.Request) {
		url, err := c.authorize()

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, url, http.StatusFound)
	})

	mux.HandleFunc(fmt.Sprintf("GET /%s/oauth/callback", provider), func(w http.ResponseWriter, r *http.Request) {
		message, err := c.callback(r)

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, HTMLResponse, message)
	})
}

func (c *controller) authorize() (string, error) {
	slog.Debug("[oauth20] authorize")
	return c.handler.GetAuthorizationURL(), nil
}

func (c *controller) callback(r *http.Request) (string, error) {
	slog.Debug("[oauth20] callback")
	ctx := r.Context()
	result, err := c.handler.Callback(ctx, r.URL.Query().Get("code"), r.URL.Query().Get("error"))

	if err != nil {
		return "", err
	}

	data, err := json.Marshal(result.Token)

	if err != nil {
		return "", err
	}

	if err := c.secretManager.Set(ctx, fmt.Sprintf("%s/oauth", c.provider), result.EntityId, string(data)); err != nil {
		return "", err
	}

	c.eventBus.Publish(ctx, shared.OrganizationCreateEvent{
		Organization: shared.Organization{
			Identifier: result.EntityId,
			Provider:   shared.PlatformProvider(c.provider),
			Name:       result.EntityName,
		},
	})

	return result.Message, nil
}
