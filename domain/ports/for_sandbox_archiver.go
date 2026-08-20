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

import "context"

// ForSandboxArchiver defines the port for archiving and restoring sandboxes.
// When a ticket is marked as done, all associated sandboxes are archived to
// free disk quota. When a ticket is reopened, the sandbox is automatically
// restored from archive when GetOrCreateSandbox is called (starting an
// archived sandbox transitions it through Restoring to Started).
type ForSandboxArchiver interface {
	// ArchiveSandbox archives the sandbox associated with the given session
	// identifier. The sandbox must be in a stopped state before archiving.
	// If no sandbox exists for the session, ArchiveSandbox is a no-op.
	ArchiveSandbox(ctx context.Context, sessionIdentifier string) error
}