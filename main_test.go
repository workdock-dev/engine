package main

import (
	"testing"

	"github.com/stretchr/testify/suite"
	daytona_types "github.com/workdock-dev/engine/plug-ins/daytona/types"
)

type MCPConfigSuite struct{ suite.Suite }

func TestMCPConfigSuite(t *testing.T) { suite.Run(t, new(MCPConfigSuite)) }

func (s *MCPConfigSuite) TestDaytonaMCPUsesHarnessConfigurationAndSecret() {
	cfg := &Config{Daytona: daytona_types.Config{
		MCPServerURL: "https://engine.example.com/api/v1/mcp/daytona",
		MCPApiKey: "internal-key",
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
