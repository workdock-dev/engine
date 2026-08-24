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

package service

import (
	"slices"
	"strings"

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

func (s *SessionConfigService) ConfigureSessionRepo(session *types.Session, labels []string, existingExternalUrls []types.ExternalURL) (*types.Session, []types.ExternalURL, bool, error) {
	repoFullName, found := s.ExtractRepoFromLabels(labels)
	if !found {
		return session, existingExternalUrls, false, nil
	}

	updated := false

	if session.RepoFullName == nil || *session.RepoFullName != repoFullName {
		session.RepoFullName = &repoFullName
		updated = true
	}

	newExternalUrls := make([]types.ExternalURL, 0, len(existingExternalUrls)+1)
	repoURL := types.GitHubUrl + repoFullName

	if slices.ContainsFunc(existingExternalUrls, func(e types.ExternalURL) bool {
		return e.URL == repoURL
	}) {
		return session, existingExternalUrls, updated, nil
	}

	found = false
	for _, ext := range existingExternalUrls {
		if ext.Label == "repo" {
			ext.URL = repoURL
			found = true
		}
		newExternalUrls = append(newExternalUrls, ext)
	}

	if !found {
		newExternalUrls = append(newExternalUrls, types.ExternalURL{
			Label: "repo",
			URL:   repoURL,
		})
	}

	return session, newExternalUrls, updated, nil
}

type GitEventFilterService struct{}

func (f *GitEventFilterService) ShouldTriggerCommentEvent(senderLogin, botLoginName string, action string) bool {
	if senderLogin == botLoginName {
		return false
	}
	if action == "deleted" {
		return false
	}
	return true
}

func (f *GitEventFilterService) ShouldTriggerCheckRunEvent(senderLogin, botLoginName, action, conclusion string) bool {
	if senderLogin == botLoginName {
		return false
	}
	if action != "completed" {
		return false
	}
	if conclusion == "" {
		return false
	}
	if conclusion != "failure" && conclusion != "timed_out" {
		return false
	}
	return true
}