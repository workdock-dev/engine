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
)

func TestEventClassificationService_IsCancelSignal(t *testing.T) {
	service := &EventClassificationService{}

	tests := []struct {
		name     string
		event    *LinearAgentSessionEvent
		expected bool
	}{
		{
			name:     "nil event",
			event:    nil,
			expected: false,
		},
		{
			name:     "nil agent activity",
			event:    &LinearAgentSessionEvent{},
			expected: false,
		},
		{
			name:     "non-stop signal",
			event:    &LinearAgentSessionEvent{AgentActivity: &LinearAgentActivity{Signal: "start"}},
			expected: false,
		},
		{
			name:     "stop signal",
			event:    &LinearAgentSessionEvent{AgentActivity: &LinearAgentActivity{Signal: "stop"}},
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := service.IsCancelSignal(test.event)
			if result != test.expected {
				t.Errorf("Expected %v, got %v", test.expected, result)
			}
		})
	}
}

func TestEventClassificationService_ClassifyInstallationEvent(t *testing.T) {
	service := &EventClassificationService{}

	tests := []struct {
		name     string
		event    *GitHubInstallationEvent
		expected InstallationAction
	}{
		{
			name:     "nil event",
			event:    nil,
			expected: InstallationAction_None,
		},
		{
			name:     "deleted action",
			event:    &GitHubInstallationEvent{Action: "deleted"},
			expected: InstallationAction_Revoke,
		},
		{
			name:     "removed action",
			event:    &GitHubInstallationEvent{Action: "removed"},
			expected: InstallationAction_Revoke,
		},
		{
			name:     "created action with repos",
			event:    &GitHubInstallationEvent{Action: "created", Repositories: []GitHubRepo{{FullName: "org/repo"}}},
			expected: InstallationAction_Grant,
		},
		{
			name:     "added action with repos",
			event:    &GitHubInstallationEvent{Action: "added", RepositoriesAdded: []GitHubRepo{{FullName: "org/repo"}}},
			expected: InstallationAction_Grant,
		},
		{
			name:     "created action without repos",
			event:    &GitHubInstallationEvent{Action: "created"},
			expected: InstallationAction_None,
		},
		{
			name:     "added repositories",
			event:    &GitHubInstallationEvent{Action: "added", RepositoriesAdded: []GitHubRepo{{FullName: "org/repo"}}},
			expected: InstallationAction_Grant,
		},
		{
			name:     "removed repositories",
			event:    &GitHubInstallationEvent{Action: "removed", RepositoriesRemoved: []GitHubRepo{{FullName: "org/repo"}}},
			expected: InstallationAction_Revoke,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := service.ClassifyInstallationEvent(test.event)
			if result != test.expected {
				t.Errorf("Expected %v, got %v", test.expected, result)
			}
		})
	}
}

func TestEventClassificationService_ShouldStoreToken(t *testing.T) {
	service := &EventClassificationService{}

	tests := []struct {
		name     string
		event    *GitHubInstallationEvent
		expected bool
	}{
		{
			name:     "grant action",
			event:    &GitHubInstallationEvent{Action: "created", Repositories: []GitHubRepo{{FullName: "org/repo"}}},
			expected: true,
		},
		{
			name:     "revoke action",
			event:    &GitHubInstallationEvent{Action: "deleted"},
			expected: false,
		},
		{
			name:     "no repos",
			event:    &GitHubInstallationEvent{Action: "created"},
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := service.ShouldStoreToken(test.event)
			if result != test.expected {
				t.Errorf("Expected %v, got %v", test.expected, result)
			}
		})
	}
}

func TestEventClassificationService_ShouldResetInstallation(t *testing.T) {
	service := &EventClassificationService{}

	tests := []struct {
		name     string
		event    *GitHubInstallationEvent
		expected bool
	}{
		{
			name:     "revoke action",
			event:    &GitHubInstallationEvent{Action: "deleted"},
			expected: true,
		},
		{
			name:     "removed repositories",
			event:    &GitHubInstallationEvent{Action: "removed", RepositoriesRemoved: []GitHubRepo{{FullName: "org/repo"}}},
			expected: true,
		},
		{
			name:     "grant action",
			event:    &GitHubInstallationEvent{Action: "created", Repositories: []GitHubRepo{{FullName: "org/repo"}}},
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := service.ShouldResetInstallation(test.event)
			if result != test.expected {
				t.Errorf("Expected %v, got %v", test.expected, result)
			}
		})
	}
}

func TestEventClassificationService_GetRepositoriesForGrant(t *testing.T) {
	service := &EventClassificationService{}

	event := &GitHubInstallationEvent{
		Repositories:      []GitHubRepo{{FullName: "org/repo1"}},
		RepositoriesAdded: []GitHubRepo{{FullName: "org/repo2"}},
	}

	repos := service.GetRepositoriesForGrant(event)

	if len(repos) != 2 {
		t.Fatalf("Expected 2 repos, got %d", len(repos))
	}

	if repos[0] != "org/repo1" || repos[1] != "org/repo2" {
		t.Errorf("Expected org/repo1 and org/repo2, got %v", repos)
	}
}

func TestEventClassificationService_GetRepositoriesForReset(t *testing.T) {
	service := &EventClassificationService{}

	event := &GitHubInstallationEvent{
		Repositories:        []GitHubRepo{{FullName: "org/repo1"}},
		RepositoriesRemoved: []GitHubRepo{{FullName: "org/repo2"}},
	}

	repos := service.GetRepositoriesForReset(event)

	if len(repos) != 1 {
		t.Fatalf("Expected 1 repo, got %d", len(repos))
	}

	if repos[0] != "org/repo2" {
		t.Errorf("Expected org/repo2, got %v", repos)
	}
}

func TestEventClassificationService_GetRepositoriesForReset_NoRemoved(t *testing.T) {
	service := &EventClassificationService{}

	event := &GitHubInstallationEvent{
		Repositories: []GitHubRepo{{FullName: "org/repo1"}},
	}

	repos := service.GetRepositoriesForReset(event)

	if len(repos) != 1 {
		t.Fatalf("Expected 1 repo, got %d", len(repos))
	}

	if repos[0] != "org/repo1" {
		t.Errorf("Expected org/repo1, got %v", repos)
	}
}
