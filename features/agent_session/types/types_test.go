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

package types

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/suite"
)

type TypesSuite struct {
	suite.Suite
}

func TestTypesSuite(t *testing.T) {
	suite.Run(t, new(TypesSuite))
}

func (s *TypesSuite) TestGenerateIdempotencyKey_Success() {
	key, err := GenerateIdempotencyKey(map[string]string{"issue": "issue-1"})

	s.Require().NoError(err)
	s.Len(key, 36)
	s.Regexp(regexp.MustCompile(`^[0-9a-f]{36}$`), key)
}

func (s *TypesSuite) TestGenerateIdempotencyKey_Deterministic() {
	payload := map[string]string{"issue": "issue-1"}

	key1, err := GenerateIdempotencyKey(payload)
	s.Require().NoError(err)

	key2, err := GenerateIdempotencyKey(payload)
	s.Require().NoError(err)

	s.Equal(key1, key2)
}

func (s *TypesSuite) TestGenerateIdempotencyKey_DifferentPayloads() {
	key1, err := GenerateIdempotencyKey(map[string]string{"issue": "issue-1"})
	s.Require().NoError(err)

	key2, err := GenerateIdempotencyKey(map[string]string{"issue": "issue-2"})
	s.Require().NoError(err)

	s.NotEqual(key1, key2)
}

func (s *TypesSuite) TestGenerateIdempotencyKey_NilPayload() {
	key, err := GenerateIdempotencyKey(nil)

	s.Error(err)
	s.Empty(key)
	s.ErrorContains(err, "expected non-nil payload for generating idempotency key, got nil")
}

func (s *TypesSuite) TestGenerateIdempotencyKey_UnmarshalablePayload() {
	key, err := GenerateIdempotencyKey(func() {})

	s.Error(err)
	s.Empty(key)
}

func (s *TypesSuite) TestEventJob_WillRetry_FalseByDefault() {
	job := EventJob{}

	s.False(job.WillRetry())
}

func (s *TypesSuite) TestSetMaxAttempts() {
	tests := []struct {
		name        string
		attempts    int
		maxAttempts int
		expected    bool
	}{
		{name: "below max retries", attempts: 0, maxAttempts: 2, expected: true},
		{name: "one left retries", attempts: 1, maxAttempts: 2, expected: true},
		{name: "at max does not retry", attempts: 2, maxAttempts: 2, expected: false},
		{name: "over max does not retry", attempts: 5, maxAttempts: 2, expected: false},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			job := EventJob{Attempts: tt.attempts}
			job.SetMaxAttempts(tt.maxAttempts)

			s.Equal(tt.expected, job.WillRetry())
		})
	}
}
