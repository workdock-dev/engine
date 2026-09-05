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

// Package file_secrets provides a file-backed implementation of
// shared.SecretManager. Secrets are persisted as one JSON file per secret
// path so values written at runtime survive process and container restarts.
// It is intended for single-node deployments such as the Compose stack.
package file_secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ModeFile is the secrets provider mode value that selects this store.
const ModeFile = "file"

// Config holds the settings used to construct a file-backed store.
type Config struct {
	RootDir string `yaml:"root_dir"`
}

const secretFilePerm = 0o600

// Store is a secret store that persists each secret path as a JSON file
// inside RootDir. Writes are serialized in-process and applied atomically.
type Store struct {
	mu      sync.Mutex
	rootDir string
}

// New returns a store rooted at the given directory, creating it if needed.
func New(rootDir string) (*Store, error) {
	if err := os.MkdirAll(rootDir, 0o700); err != nil {
		return nil, fmt.Errorf("[secret-manager][file] failed to create root dir: %w", err)
	}

	return &Store{rootDir: rootDir}, nil
}

// Get returns the stored value for the given path and name.
func (s *Store) Get(ctx context.Context, secretPath, secretName string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	dir, err := resolveDir(s.rootDir, secretPath)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	names, err := readFile(dir)
	if err != nil {
		return "", err
	}

	value, ok := names[secretName]

	if !ok {
		return "", fmt.Errorf("secret not found: path=%s name=%s", secretPath, secretName)
	}

	return value, nil
}

// Set stores the value for the given path and name, replacing any existing
// one. The change is persisted atomically via a temp file and rename.
func (s *Store) Set(ctx context.Context, secretPath, secretName, secretValue string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	dir, err := resolveDir(s.rootDir, secretPath)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	names, err := readFile(dir)

	if err != nil {
		return err
	}

	names[secretName] = secretValue

	return writeFile(dir, names)
}

// Delete removes the value for the given path and name. A missing secret is
// treated as already deleted (no error).
func (s *Store) Delete(ctx context.Context, secretPath, secretName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	dir, err := resolveDir(s.rootDir, secretPath)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	names, err := readFile(dir)

	if err != nil {
		return err
	}

	if _, ok := names[secretName]; !ok {
		return nil
	}

	delete(names, secretName)

	return writeFile(dir, names)
}

// resolveDir maps a secret path to a directory under rootDir. A leading
// slash is allowed for compatibility with Infisical-style paths. Empty
// paths, parent traversal, and empty segments are rejected.
func resolveDir(rootDir, secretPath string) (string, error) {
	trimmed := strings.TrimPrefix(secretPath, "/")

	if trimmed == "" {
		return "", fmt.Errorf("[secret-manager][file] invalid secret path: %q", secretPath)
	}

	for _, segment := range strings.Split(trimmed, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("[secret-manager][file] invalid secret path: %q", secretPath)
		}
	}

	return filepath.Join(rootDir, filepath.FromSlash(trimmed)), nil
}

// readFile loads the JSON file backing a secret path directory. A missing
// file is treated as an empty path.
func readFile(dir string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "secrets.json"))

	if os.IsNotExist(err) {
		return make(map[string]string), nil
	}

	if err != nil {
		return nil, fmt.Errorf("[secret-manager][file] failed to read secrets: %w", err)
	}

	names := make(map[string]string)

	if err := json.Unmarshal(data, &names); err != nil {
		return nil, fmt.Errorf("[secret-manager][file] failed to parse secrets: %w", err)
	}

	return names, nil
}

// writeFile persists the JSON file backing a secret path directory,
// creating the directory as needed and replacing the file atomically.
func writeFile(dir string, names map[string]string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("[secret-manager][file] failed to create secret path dir: %w", err)
	}

	target := filepath.Join(dir, "secrets.json")
	temp, err := os.CreateTemp(dir, ".secrets-*.json")

	if err != nil {
		return fmt.Errorf("[secret-manager][file] failed to create temp file: %w", err)
	}

	tempName := temp.Name()

	defer os.Remove(tempName)

	if err := os.Chmod(tempName, secretFilePerm); err != nil {
		temp.Close()
		return fmt.Errorf("[secret-manager][file] failed to set file permissions: %w", err)
	}

	encoder := json.NewEncoder(temp)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(names); err != nil {
		temp.Close()
		return fmt.Errorf("[secret-manager][file] failed to encode secrets: %w", err)
	}

	if err := temp.Close(); err != nil {
		return fmt.Errorf("[secret-manager][file] failed to write secrets: %w", err)
	}

	if err := os.Rename(tempName, target); err != nil {
		return fmt.Errorf("[secret-manager][file] failed to persist secrets: %w", err)
	}

	return nil
}
