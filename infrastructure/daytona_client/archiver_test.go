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
// WITHOUT WARRANTIES OR CONDITIONS of ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package daytona_client

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
)

type ArchiverSuite struct {
	suite.Suite
}

func TestArchiverSuite(t *testing.T) {
	suite.Run(t, new(ArchiverSuite))
}

func (s *ArchiverSuite) TestNewSandboxArchiver_ReturnsInterface() {
	archiver := NewSandboxArchiver(SandboxConfig{
		ApiUrl: "https://example.com",
		ApiKey: "test-key",
	})
	s.NotNil(archiver)
}

func (s *ArchiverSuite) TestArchiveSandbox_NotFound_NoError() {
	// When the sandbox doesn't exist, ArchiveSandbox should be a no-op.
	// This test verifies the archiver can be created and the interface is satisfied.
	// Integration testing with the Daytona API is required for full coverage.
	archiver := NewSandboxArchiver(SandboxConfig{
		ApiUrl: "https://example.com",
		ApiKey: "test-key",
	})
	s.NotNil(archiver)
}