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
)

const (
	LinearAccessTokenEnvVar = "LINEAR_ACCESS_TOKEN"
	LinearMCPType          = "remote"
	LinearMCPURL           = "https://mcp.linear.app/mcp"
)

type AgentConfig struct {
	Schema     string         `json:"$schema"`
	Share      string         `json:"share"`
	AutoUpdate bool           `json:"autoupdate"`
	Permission map[string]any `json:"permission"`
	Provider   map[string]any `json:"provider"`
	MCP        map[string]any `json:"mcp"`
}

type AgentConfigFactory struct{}

func NewAgentConfigFactory() *AgentConfigFactory {
	return &AgentConfigFactory{}
}

type OpenCodeConfigInput struct {
	Permission map[string]any
	Provider   map[string]any
}

func (f *AgentConfigFactory) BuildOpenCodeConfig(input OpenCodeConfigInput) (*AgentConfig, error) {
	permission := input.Permission
	if permission == nil {
		permission = map[string]any{
			"*": "allow",
		}
	}

	provider := input.Provider
	if provider == nil {
		provider = map[string]any{}
	}

	mcp := map[string]any{
		"linear": map[string]any{
			"type":    LinearMCPType,
			"url":     LinearMCPURL,
			"enabled": true,
			"oauth":   false,
			"headers": map[string]string{
				"Authorization": fmt.Sprintf("Bearer {env:%s}", LinearAccessTokenEnvVar),
			},
		},
	}

	return &AgentConfig{
		Schema:     "https://opencode.ai/config.json",
		Share:      "disabled",
		AutoUpdate: false,
		Permission: permission,
		Provider:   provider,
		MCP:        mcp,
	}, nil
}

func (f *AgentConfigFactory) SerializeConfig(config *AgentConfig) ([]byte, error) {
	configJSON := fmt.Sprintf(`{
  "$schema": "https://opencode.ai/config.json",
  "share": "disabled",
  "autoupdate": false,
  "permission": %s,
  "provider": %s,
  "mcp": {
    %s
  }
}`,
		mustMarshalJSON(config.Permission),
		mustMarshalJSON(config.Provider),
		formatLinearMCP(config.MCP["linear"].(map[string]any)),
	)

	return []byte(configJSON), nil
}

func mustMarshalJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func formatLinearMCP(mcp map[string]any) string {
	headers := mcp["headers"].(map[string]string)
	return fmt.Sprintf(`"linear": {
      "type": "remote",
      "url": "https://mcp.linear.app/mcp",
      "enabled": true,
      "oauth": false,
      "headers": {
        "Authorization": "Bearer {env:%s}"
      }
    }`, LinearAccessTokenEnvVar)
}
