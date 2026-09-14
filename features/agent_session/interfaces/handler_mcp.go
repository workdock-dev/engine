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

type MCPConfig struct {
	Name             string
	Url              string
	AuthHeaderKey    string // Defines the http auth header key e.g. Authorization, X-Api-Key
	AuthHeaderValue  string // Defines the http auth header value e.g. Bearer {env:SOME_SCRET_VAR}, SOME_SECRET_VAR, this will always point to an env var
	AuthSecretEnvVar string // Defines the env var where the secret is e.g. SOME_SECRET_VAR
	AuthSecret       string // The secret itself that will be assign or shadow-assign to the SOME_SECRET_VAR
	Hosts            []string
}

type HandlerMCP interface {
	GetMCPList() []MCPConfig
}
