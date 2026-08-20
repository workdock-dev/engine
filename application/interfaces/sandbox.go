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

package interfaces

import (
	"context"
	"time"
)

type Sandbox interface {
	GetOrCreateSandbox(ctx context.Context, secrets, envVars map[string]string) (bool, error)
	UpdateExistingSandbox(ctx context.Context, secrets, envVars map[string]string) error
	// SetSecret return secret id, secret name, error
	SetSecret(ctx context.Context, secretValue string, hosts []string) (string, string, error)
	DeleteSecret(ctx context.Context, secretId string) error
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
	UploadFile(ctx context.Context, data []byte, path string) error
	UpdateEnv(ctx context.Context, envVars map[string]string) error
	ConfigureGitUser(ctx context.Context, name, email string) error
	ExecuteCommand(ctx context.Context, command string, timeout time.Duration) (string, error)
	CreateExecutionSession(ctx context.Context) error
	DeleteExecutionSession(ctx context.Context) error
	ExecuteSessionCommand(ctx context.Context, command string) (map[string]any, error)
	StreamSessionCommandLogs(ctx context.Context, cmdId string, stdout chan<- string, stderr chan<- string) error
	DeleteSandbox(ctx context.Context) error
}
