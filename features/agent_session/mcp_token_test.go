package agent_session

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/suite"
)

type MCPTokenSuite struct{ suite.Suite }

func TestMCPTokenSuite(t *testing.T) { suite.Run(t, new(MCPTokenSuite)) }

func (s *MCPTokenSuite) TestNewMCPToken() {
	pattern := regexp.MustCompile(`^[0-9a-f]{64}$`)
	first, err := newMCPToken()
	s.Require().NoError(err)
	s.Regexp(pattern, first)
	second, err := newMCPToken()
	s.Require().NoError(err)
	s.Regexp(pattern, second)
	s.NotEqual(first, second)
}
