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
	"testing"

	"github.com/stretchr/testify/suite"
)

type AgentConfigFactorySuite struct {
	suite.Suite
}

func TestAgentConfigFactorySuite(t *testing.T) {
	suite.Run(t, new(AgentConfigFactorySuite))
}

func (s *AgentConfigFactorySuite) TestBuildOpenCodeConfig_DefaultPermission() {
	factory := AgentConfigFactory{}

	config, err := factory.BuildOpenCodeConfig(nil, nil)

	s.NoError(err)
	s.NotNil(config)
	s.NotNil(config.Permission)
	s.Equal("allow", config.Permission["*"])
	s.NotNil(config.MCP.Linear)
	s.Equal("https://mcp.linear.app/mcp", config.MCP.Linear.URL)
	s.True(config.MCP.Linear.Enabled)
}

func (s *AgentConfigFactorySuite) TestBuildOpenCodeConfig_CustomPermission() {
	factory := AgentConfigFactory{}
	permission := PermissionRule{
		"*":                  "allow",
		"external_directory": "deny",
	}

	config, err := factory.BuildOpenCodeConfig(permission, nil)

	s.NoError(err)
	s.NotNil(config)
	s.Equal("allow", config.Permission["*"])
	s.Equal("deny", config.Permission["external_directory"])
}

func (s *AgentConfigFactorySuite) TestBuildOpenCodeConfig_CustomProvider() {
	factory := AgentConfigFactory{}
	provider := Provider{
		"openai": map[string]any{
			"model": "gpt-4o",
		},
	}

	config, err := factory.BuildOpenCodeConfig(nil, provider)

	s.NoError(err)
	s.NotNil(config)
	s.NotNil(config.Provider)
}

func (s *AgentConfigFactorySuite) TestMarshalOpenCodeConfig() {
	factory := AgentConfigFactory{}
	config := &OpenCodeAgentConfig{
		Permission: PermissionRule{
			"*": "allow",
		},
		Provider: Provider{
			"openai": map[string]any{
				"model": "gpt-4o",
			},
		},
		MCP: MCPConfig{
			Linear: MCPProvider{
				Type:    "remote",
				URL:     "https://mcp.linear.app/mcp",
				Enabled: true,
				OAuth:   false,
				Headers: map[string]string{
					"Authorization": "Bearer {env:LINEAR_ACCESS_TOKEN}",
				},
			},
		},
	}

	data, err := factory.MarshalOpenCodeConfig(config)

	s.NoError(err)
	s.NotEmpty(data)
	s.Contains(string(data), "opencode.ai/config.json")
	s.Contains(string(data), "permission")
	s.Contains(string(data), "mcp")
}

func (s *AgentConfigFactorySuite) TestParseModelInfo_WithSlash() {
	factory := AgentConfigFactory{}

	provider, model := factory.ParseModelInfo("openai/gpt-4o")

	s.Equal("openai", provider)
	s.Equal("gpt-4o", model)
}

func (s *AgentConfigFactorySuite) TestParseModelInfo_WithoutSlash() {
	factory := AgentConfigFactory{}

	provider, model := factory.ParseModelInfo("gpt-4o")

	s.Equal("unknown", provider)
	s.Equal("gpt-4o", model)
}

func (s *AgentConfigFactorySuite) TestParseModelInfo_Empty() {
	factory := AgentConfigFactory{}

	provider, model := factory.ParseModelInfo("")

	s.Equal("unknown", provider)
	s.Equal("unknown", model)
}
