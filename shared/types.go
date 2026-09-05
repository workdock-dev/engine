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

package shared

type PlatformProvider string
type HarnessProvider string

const (
	PlatformProvider_Linear  PlatformProvider = "linear"
	PlatformProvider_GitHub  PlatformProvider = "github"
	PlatformProvider_Daytona PlatformProvider = "daytona"

	HarnessProvider_OpenCode HarnessProvider = "opencode"
)

type Organization struct {
	Identifier string
	Provider   PlatformProvider
	Name       string
}
