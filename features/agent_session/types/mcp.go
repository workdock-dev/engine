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

package types

type WorkReportInput struct {
	Session      string   `json:"session" jsonschema:"Read the exact value from AGENT_SESSION_CONFIG."`
	LinesAdded   int      `json:"linesAdded" jsonschema:"Lines of code added; zero when none."`
	LinesRemoved int      `json:"linesRemoved" jsonschema:"Lines of code removed; zero when none."`
	Commits      []string `json:"commits" jsonschema:"Commit SHAs; an empty list when none."`
	Report       string   `json:"report" jsonschema:"Summary of work, next action, or questions; at most 280 characters."`
}

type CreatePullRequestInput struct {
	Session string `json:"session" jsonschema:"Read the exact value from AGENT_SESSION_CONFIG."`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Head    string `json:"head"`
	Base    string `json:"base"`
	Draft   bool   `json:"draft,omitempty"`
}
