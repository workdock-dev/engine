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

// ModelConfig describes a model entry of a custom provider in
// ~/.pi/agent/models.json. Only id is required; the rest defaults to pi's
// built-in values. https://pi.dev/docs/latest/models
type ModelConfig struct {
	Id            string `yaml:"id"`
	Name          string `yaml:"name,omitempty"`
	Reasoning     bool   `yaml:"reasoning,omitempty"`
	ContextWindow int    `yaml:"context_window,omitempty"`
	MaxTokens     int    `yaml:"max_tokens,omitempty"`
}

// ProviderConfig describes a custom provider declared in
// ~/.pi/agent/models.json. The api_key supports pi's config value syntax,
// including "$ENV_VAR" interpolation. https://pi.dev/docs/latest/models
type ProviderConfig struct {
	Name    string        `yaml:"name"`
	BaseUrl string        `yaml:"base_url"`
	Api     string        `yaml:"api"`
	ApiKey  string        `yaml:"api_key"`
	Models  []ModelConfig `yaml:"models"`
}

type Config struct {
	Version       string          `yaml:"version"`
	ThinkingLevel string          `yaml:"thinking_level"`
	Tools         []string        `yaml:"tools"`
	Provider      *ProviderConfig `yaml:"provider"`
}
