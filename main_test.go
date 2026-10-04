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

package main

import (
	"testing"

	"github.com/stretchr/testify/suite"
	daytona_types "github.com/workdock-dev/engine/plug-ins/daytona/types"
)

type MCPConfigSuite struct {
	suite.Suite
}

func TestMCPConfigSuite(t *testing.T) {
	suite.Run(t, new(MCPConfigSuite))
}

func (s *MCPConfigSuite) TestDaytonaMCPUsesHarnessConfigurationAndSecret() {
	cfg := &Config{Daytona: daytona_types.Config{
		MCPServerURL: "https://engine.example.com/api/v1/mcp/daytona",
		MCPApiKey:    "internal-key",
	}}
	list := (&MCPFromConfigFile{config: cfg}).GetMCPList()
	s.Require().Len(list, 1)
	s.Equal("daytona", list[0].Name)
	s.Equal(cfg.Daytona.MCPServerURL, list[0].Url)
	s.Equal("Authorization", list[0].AuthHeaderKey)
	s.Equal("Bearer {env:WORKDOCK_DAYTONA_MCP_API_KEY}", list[0].AuthHeaderValue)
	s.Equal("WORKDOCK_DAYTONA_MCP_API_KEY", list[0].AuthSecretEnvVar)
	s.Equal("internal-key", list[0].AuthSecret)
	s.Equal([]string{"engine.example.com"}, list[0].Hosts)
}

func (s *MCPConfigSuite) TestDaytonaMCPIsNotConfiguredWithoutAPIKeyOrURL() {
	for _, cfg := range []*Config{
		{Daytona: daytona_types.Config{MCPServerURL: "https://engine.example.com/mcp"}},
		{Daytona: daytona_types.Config{MCPApiKey: "internal-key"}},
	} {
		s.Empty((&MCPFromConfigFile{config: cfg}).GetMCPList())
	}
}
