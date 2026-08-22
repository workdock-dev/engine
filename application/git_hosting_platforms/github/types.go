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

package github

import (
	"time"
)

const (
	GitHub_SecretPath = "/github/installations"
)

type InstallationAccessToken struct {
	Token               string    `json:"token"`
	ExpiresAt           time.Time `json:"expires_at"`
	Permissions         any       `json:"permissions"`
	RepositorySelection string    `json:"repository_selection"`
}

type WebhookEvent struct {
	DeliveryID          string        `json:"-"`
	EventType           string        `json:"event_type"`
	Action              string        `json:"action"`
	Installation        *Installation `json:"installation,omitempty"`
	Repositories        []Repository  `json:"repositories,omitempty"`
	RepositoriesAdded   []Repository  `json:"repositories_added,omitempty"`
	RepositoriesRemoved []Repository  `json:"repositories_removed,omitempty"`
	Sender              *User         `json:"sender,omitempty"`
	PullRequest         *PullRequest  `json:"pull_request"`
	CheckRun            *CheckRun     `json:"check_run,omitempty"`
	CheckSuite          *CheckSuite   `json:"check_suite,omitempty"`
}

type Installation struct {
	ID      int    `json:"id"`
	NodeID  string `json:"node_id"`
	Account *User  `json:"account,omitempty"`
}

type Repository struct {
	ID       int    `json:"id"`
	NodeID   string `json:"node_id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
}

type User struct {
	Login string `json:"login"`
	ID    int    `json:"id"`
}

type PullRequest struct {
	Head   Head   `json:"head"`
	Number int    `json:"number"`
	URL    string `json:"url"`
	Title  string `json:"title"`
}

type Head struct {
	Ref  string `json:"ref"`
	SHA  string `json:"sha"`
	Repo Repo   `json:"repo"`
}

type Repo struct {
	FullName string `json:"full_name"`
}

type CheckRun struct {
	ID           int64         `json:"id"`
	NodeID       string        `json:"node_id"`
	HeadBranch   string        `json:"head_branch"`
	HeadSHA      string        `json:"head_sha"`
	Status       string        `json:"status"`
	Conclusion   *string       `json:"conclusion"`
	URL          string        `json:"url"`
	CheckSuite   *CheckSuite   `json:"check_suite,omitempty"`
	PullRequests []PullRequest `json:"pull_requests,omitempty"`
}

type CheckSuite struct {
	ID         int64   `json:"id"`
	NodeID     string  `json:"node_id"`
	HeadBranch string  `json:"head_branch"`
	HeadSHA    string  `json:"head_sha"`
	Status     string  `json:"status"`
	Conclusion *string `json:"conclusion"`
}
