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

package ports

import (
	"context"

	"github.com/workdock-dev/engine/domain/types"
)

// ForGitHostingPlatform defines the port for integrating with Git hosting
// platforms such as GitHub, GitLab, and Bitbucket.
type ForGitHostingPlatform interface {
	// Ingest transforms a Git hosting webhook event into a domain event.
	Ingest(ctx context.Context, event any) error

	// VerifyRepoAccess verifies whether the platform connection associated
	// with the session has access to the specified repository.
	VerifyRepoAccess(ctx context.Context, sessionEventIdentifier string, repo *string) (bool, string, error)

	// RequestConnection requests access to the specified repository.
	RequestConnection(ctx context.Context, sessionEventIdentifier, repo string) error

	ForWebhooks
}

type GitHostingPlatformRegistry = map[types.PlatformProvider]ForGitHostingPlatform
