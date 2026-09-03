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

	"github.com/workdock-dev/engine/shared"
)

type SandboxShutdown = func(ctx context.Context) string

type SandboxSecret struct {
	Name  string   `yaml:"name"`
	Value string   `yaml:"value"`
	Hosts []string `yaml:"hosts"`
}

type SandboxConfig struct {
	AutoStopInterval    int
	Session             *shared.Session
	SessionEvent        *shared.SessionEvent
	Secrets             []SandboxSecret
	FileUploads         map[string][]byte
	CommandsWhenCreated []string
	Commands            []string
	ExitCommand         string
	HarnessCommand      string
	GitName             string
	GitEmail            string
}

// HandlerSandbox is the interfaces through which the sandbox will be
// executed or Archive
type HandlerSandbox interface {
	// Run the sandbox with all it's related configuration. Receives channel
	// to listen to the harness output. The sandbox implementation is responsible
	// of closing the channels
	//
	// returns the shutdown function which may return the exit command result
	Run(
		ctx context.Context,
		config *SandboxConfig,
		stdout chan<- string,
		stderr chan<- string,
	) (SandboxShutdown, error)

	// Archive a given sandbox
	Archive(ctx context.Context, config *SandboxConfig) error
}
