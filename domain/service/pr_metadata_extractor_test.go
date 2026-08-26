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

package domain_service

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type SessionResultServiceSuite struct {
	suite.Suite
}

func TestSessionResultServiceSuite(t *testing.T) {
	suite.Run(t, new(SessionResultServiceSuite))
}

func (s *SessionResultServiceSuite) TestParsePullRequestMetadata_ValidJSON() {
	service := SessionResultService{}
	jsonStr := `{"headRefName":"feature-branch","headRefOid":"abc123","number":42,"url":"https://github.com/owner/repo/pull/42"}`

	pr := service.ParsePullRequestMetadata(jsonStr)

	s.NotNil(pr)
	s.Equal("feature-branch", pr.HeadRefName)
	s.Equal("abc123", pr.HeadRefOID)
	s.Equal(42, pr.Number)
	s.Equal("https://github.com/owner/repo/pull/42", pr.URL)
}

func (s *SessionResultServiceSuite) TestParsePullRequestMetadata_EmptyString() {
	service := SessionResultService{}

	pr := service.ParsePullRequestMetadata("")

	s.Nil(pr)
}

func (s *SessionResultServiceSuite) TestParsePullRequestMetadata_InvalidJSON() {
	service := SessionResultService{}

	pr := service.ParsePullRequestMetadata("not valid json")

	s.Nil(pr)
}
