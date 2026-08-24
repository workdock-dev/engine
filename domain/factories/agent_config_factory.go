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

package factories

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/workdock-dev/engine/domain/types"
)

type AgentConfigFactory struct{}

type PermissionRule map[string]any
type ConfigExternal struct {
	Model               string                `yaml:"model"`
	Permission          PermissionRule        `yaml:"permission"`
	Secrets             map[string]SecretSpec `yaml:"secrets"`
	Provider            map[string]any        `yaml:"provider"`
	DestroyOnDispose    bool                  `yaml:"destroy_on_dispose"`
	LivenessTimeoutSecs int                   `yaml:"liveness_timeout_seconds"`
	MaxHealthMisses     int                   `yaml:"max_health_misses"`
}

type SecretSpec struct {
	Value string   `yaml:"value"`
	Hosts []string `yaml:"hosts"`
}

type OpenCodeAgentConfig struct {
	Permission PermissionRule
	Provider   map[string]any
	MCP        MCPConfig
}

type MCPConfig struct {
	Linear MCPProvider `json:"linear"`
}

type MCPProvider struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Enabled bool              `json:"enabled"`
	OAuth   bool              `json:"oauth"`
	Headers map[string]string `json:"headers"`
}

const (
	LinearAccessTokenEnvVar = "LINEAR_ACCESS_TOKEN"
	LinearMCPURL            = "https://mcp.linear.app/mcp"
)

func (f *AgentConfigFactory) BuildOpenCodeConfig(permission PermissionRule, provider map[string]any) (*OpenCodeAgentConfig, error) {
	if permission == nil {
		permission = PermissionRule{
			"*": "allow",
		}
	}

	mcp := MCPConfig{
		Linear: MCPProvider{
			Type:    "remote",
			URL:     LinearMCPURL,
			Enabled: true,
			OAuth:   false,
			Headers: map[string]string{
				"Authorization": fmt.Sprintf("Bearer {env:%s}", LinearAccessTokenEnvVar),
			},
		},
	}

	config := &OpenCodeAgentConfig{
		Permission: permission,
		Provider:   provider,
		MCP:        mcp,
	}

	return config, nil
}

func (f *AgentConfigFactory) MarshalOpenCodeConfig(config *OpenCodeAgentConfig) ([]byte, error) {
	permissionStr, err := json.Marshal(config.Permission)
	if err != nil {
		slog.Error("failed to marshal opencode permissions", "err", err)
		return nil, err
	}

	providerStr := "{}"
	if config.Provider != nil {
		d, err := json.Marshal(config.Provider)
		if err != nil {
			slog.Error("failed to marshal opencode providers", "err", err)
			return nil, err
		}
		providerStr = string(d)
	}

	linearMCP := config.MCP.Linear
	mcpJSON := fmt.Sprintf(`"linear": {
      "type": "%s",
      "url": "%s",
      "enabled": %t,
      "oauth": %t,
      "headers": {
        "Authorization": "%s"
      }
    }`,
		linearMCP.Type,
		linearMCP.URL,
		linearMCP.Enabled,
		linearMCP.OAuth,
		linearMCP.Headers["Authorization"],
	)

	openCodeConfig := fmt.Appendf(nil, `
{
  "$schema": "https://opencode.ai/config.json",
  "share": "disabled",
  "autoupdate": false,
  "permission": %s,
  "provider": %s,
  "mcp": {
    %s
  }
}
	`, string(permissionStr), providerStr, mcpJSON)

	return openCodeConfig, nil
}

func (f *AgentConfigFactory) ParseModelInfo(model string) (provider string, modelName string) {
	if model == "" {
		return "unknown", "unknown"
	}

	before, after, found := strings.Cut(model, "/")
	if found {
		return before, after
	}
	return "unknown", "unknown"
}