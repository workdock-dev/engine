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
	"slices"
	"strings"

	"github.com/workdock-dev/engine/domain/types"
)

const GitHubUrl = "https://github.com/"

type SessionConfigService struct{}

func NewSessionConfigService() *SessionConfigService {
	return &SessionConfigService{}
}

type Label struct {
	Name string
}

type ConfigureSessionRepoInput struct {
	Session       *types.Session
	Labels        []Label
	ExistingURLs  []types.ExternalURL
}

type ConfigureSessionRepoOutput struct {
	UpdatedSession *types.Session
	UpdatedURLs   []types.ExternalURL
	RepoFound     bool
}

func (s *SessionConfigService) ConfigureSessionRepo(input ConfigureSessionRepoInput) ConfigureSessionRepoOutput {
	output := ConfigureSessionRepoOutput{
		UpdatedSession: input.Session,
		UpdatedURLs:    input.ExistingURLs,
		RepoFound:      false,
	}

	for _, label := range input.Labels {
		if after, ok := strings.CutPrefix(label.Name, "repo="); ok {
			repoFullName := after
			output.RepoFound = true

			if output.UpdatedSession.RepoFullName == nil || *output.UpdatedSession.RepoFullName != repoFullName {
				output.UpdatedSession.RepoFullName = &repoFullName
			}

			if slices.ContainsFunc(output.UpdatedURLs, func(e types.ExternalURL) bool {
				return e.URL == GitHubUrl+repoFullName
			}) {
				continue
			}

			found := false
			updated := make([]types.ExternalURL, 0, len(output.UpdatedURLs)+1)

			for _, ext := range output.UpdatedURLs {
				if ext.Label == "repo" {
					ext.URL = GitHubUrl + repoFullName
					found = true
				}
				updated = append(updated, ext)
			}

			if !found {
				updated = append(updated, types.ExternalURL{
					Label: "repo",
					URL:   GitHubUrl + repoFullName,
				})
			}

			output.UpdatedURLs = updated
		}
	}

	return output
}
