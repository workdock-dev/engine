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

package linear

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	oauth20 "github.com/workdock-dev/engine/features/oauth2.0"
	"github.com/workdock-dev/engine/plug-ings/linear/interfaces"
	"github.com/workdock-dev/engine/plug-ings/linear/types"
	"github.com/workdock-dev/engine/shared"
)

const (
	AuthorizeEndpoint     = "https://linear.app/oauth/authorize"
	ExchangeTokenEndpoint = "https://api.linear.app/oauth/token"
)

type oauth20Handler struct {
	config types.Config
	client interfaces.Client
}

func NewOAuth20Handler(config types.Config, client interfaces.Client) oauth20.OauthHandler {
	return &oauth20Handler{
		config: config,
		client: client,
	}
}

func (h *oauth20Handler) GetAuthorizationURL() string {
	scope := "read,write,app:assignable,app:mentionable"
	return fmt.Sprintf(
		"%s?client_id=%s&redirect_uri=%s/linear/oauth/callback&response_type=code&scope=%s&actor=app",
		AuthorizeEndpoint, h.config.ClientId, h.config.ServerUrl, scope,
	)

}

func (h *oauth20Handler) Callback(ctx context.Context, code, errCode string) (*oauth20.CallbackResult, error) {
	if errCode != "" {
		slog.Error("failed to handle linear oauth callback", "err", errors.New(errCode))
		return nil, shared.ErrBadRequest
	}

	if code == "" {
		slog.Error("failed to handle linear oauth callback", "err", errors.New("missing required oauth parameter: code"))
		return nil, shared.ErrBadRequest
	}

	tokenData, err := h.client.ExchangeCode(ctx, code)

	if err != nil {
		return nil, err
	}

	info, err := h.client.GetWorkspaceInfo(ctx, tokenData.AccessToken)

	if err != nil {
		return nil, err
	}

	token := types.Token{
		AccessToken:  tokenData.AccessToken,
		RefreshToken: tokenData.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenData.ExpiresIn) * time.Second),
	}

	slog.Debug("oauth authorization successful", "workspace", info.Name)
	return &oauth20.CallbackResult{
		EntityId:   info.ID,
		EntityName: info.Name,
		Message:    info.Name,
		Token: oauth20.Token{
			AccessToken:  token.AccessToken,
			RefreshToken: token.RefreshToken,
			ExpiresAt:    token.ExpiresAt,
		},
	}, nil
}
