package ports

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

// SandboxHandler is the interfaces through which the sandbox will be
// executed or Archive
type SandboxHandler interface {
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
