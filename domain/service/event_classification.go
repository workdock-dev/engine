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

type EventClassificationService struct{}

type LinearAgentActivity struct {
	Signal string
}

type LinearAgentSessionEvent struct {
	AgentActivity *LinearAgentActivity
}

func (s *EventClassificationService) IsCancelSignal(event *LinearAgentSessionEvent) bool {
	if event == nil || event.AgentActivity == nil {
		return false
	}

	return event.AgentActivity.Signal == "stop"
}

type GitHubInstallationEvent struct {
	Action              string
	InstallationID      string
	Repositories        []GitHubRepo
	RepositoriesAdded   []GitHubRepo
	RepositoriesRemoved []GitHubRepo
}

type GitHubRepo struct {
	FullName string
}

type InstallationAction int

const (
	InstallationAction_None InstallationAction = iota
	InstallationAction_Grant
	InstallationAction_Revoke
	InstallationAction_UpdateRepositories
)

func (s *EventClassificationService) ClassifyInstallationEvent(event *GitHubInstallationEvent) InstallationAction {
	if event == nil {
		return InstallationAction_None
	}

	switch event.Action {
	case "deleted":
		return InstallationAction_Revoke
	case "created":
		if len(event.Repositories) > 0 || len(event.RepositoriesAdded) > 0 {
			return InstallationAction_Grant
		}
		return InstallationAction_None
	case "added":
		if len(event.RepositoriesAdded) > 0 {
			return InstallationAction_UpdateRepositories
		}
		if len(event.Repositories) > 0 {
			return InstallationAction_Grant
		}
		return InstallationAction_None
	case "removed":
		if len(event.RepositoriesRemoved) > 0 {
			return InstallationAction_UpdateRepositories
		}
		return InstallationAction_Revoke
	}

	return InstallationAction_None
}

func (s *EventClassificationService) ShouldStoreToken(event *GitHubInstallationEvent) bool {
	action := s.ClassifyInstallationEvent(event)
	return action == InstallationAction_Grant
}

func (s *EventClassificationService) ShouldResetInstallation(event *GitHubInstallationEvent) bool {
	action := s.ClassifyInstallationEvent(event)
	return action == InstallationAction_Revoke || action == InstallationAction_UpdateRepositories
}

func (s *EventClassificationService) GetRepositoriesForGrant(event *GitHubInstallationEvent) []string {
	if event == nil {
		return nil
	}

	repos := make([]string, 0)

	for _, repo := range event.Repositories {
		repos = append(repos, repo.FullName)
	}

	for _, repo := range event.RepositoriesAdded {
		repos = append(repos, repo.FullName)
	}

	return repos
}

func (s *EventClassificationService) GetRepositoriesForReset(event *GitHubInstallationEvent) []string {
	if event == nil {
		return nil
	}

	repos := make([]string, 0)

	for _, repo := range event.RepositoriesRemoved {
		repos = append(repos, repo.FullName)
	}

	if len(repos) == 0 {
		for _, repo := range event.Repositories {
			repos = append(repos, repo.FullName)
		}
	}

	return repos
}
