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

package agent_session

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/suite"
)

type MCPTokenSuite struct {
	suite.Suite
}

func TestMCPTokenSuite(t *testing.T) {
	suite.Run(t, new(MCPTokenSuite))
}

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

func (s *MCPTokenSuite) TestNewMCPToken_DistinctExecutions() {
	seen := make(map[string]bool)

	for range 128 {
		token, err := newMCPToken()

		s.Require().NoError(err)
		s.Regexp(`^[0-9a-f]{64}$`, token)
		s.NotContains(seen, token)
		seen[token] = true
	}
}
