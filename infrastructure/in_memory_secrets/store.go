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

// Package in_memory_secrets provides an in-process implementation of
// ports.ForSecrets. It is intended for development and benchmarking so that
// the engine can run without reaching an external secrets manager.
package in_memory_secrets

import (
	"context"
	"fmt"
	"maps"
	"sync"
)

// ModeMemory is the secrets provider mode value that selects this store.
const ModeMemory = "memory"

// Store is a thread-safe, process-local secret store keyed by (path, name).
type Store struct {
	mu      sync.RWMutex
	secrets map[string]map[string]string
}

// New returns an empty store.
func New() *Store {
	return &Store{
		secrets: make(map[string]map[string]string),
	}
}

// NewWithSeeds returns a store pre-populated with the given secrets, where
// seeds is keyed by secret path and then by secret name.
func NewWithSeeds(seeds map[string]map[string]string) *Store {
	s := New()

	for path, names := range seeds {
		if s.secrets[path] == nil {
			s.secrets[path] = make(map[string]string)
		}

		maps.Copy(s.secrets[path], names)
	}

	return s
}

// Get returns the stored value for the given path and name.
func (s *Store) Get(ctx context.Context, secretPath, secretName string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	value, ok := s.secrets[secretPath][secretName]

	if !ok {
		return "", fmt.Errorf("secret not found: path=%s name=%s", secretPath, secretName)
	}

	return value, nil
}

// Set stores the value for the given path and name, replacing any existing one.
func (s *Store) Set(ctx context.Context, secretPath, secretName, secretValue string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	names := s.secrets[secretPath]

	if names == nil {
		names = make(map[string]string)
		s.secrets[secretPath] = names
	}

	names[secretName] = secretValue
	return nil
}

// Delete removes the value for the given path and name. A missing secret is
// treated as already deleted (no error).
func (s *Store) Delete(ctx context.Context, secretPath, secretName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	names := s.secrets[secretPath]

	if names == nil {
		return nil
	}

	delete(names, secretName)
	return nil
}
