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
	"context"
	"strings"

	"github.com/workdock-dev/engine/domain/repositories"
	"github.com/workdock-dev/engine/domain/types"
)

type SessionConfigService struct{}

func (s *SessionConfigService) ExtractRepoFromLabels(labels []string) (string, bool) {
	for _, label := range labels {
		if after, ok := strings.CutPrefix(label, "repo="); ok {
			return after, true
		}
	}
	return "", false
}

func (s *SessionConfigService) ConfigureSessionRepo(ctx context.Context, session *types.Session, labels []string, sessionRepo repositories.SessionRepository) error {
	repoFullName, found := s.ExtractRepoFromLabels(labels)
	if !found {
		return nil
	}

	needsUpdate := session.RepoFullName == nil || *session.RepoFullName != repoFullName
	if needsUpdate {
		session.RepoFullName = &repoFullName
		return sessionRepo.UpsertAgentSession(ctx, session)
	}

	return nil
}