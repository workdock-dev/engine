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

package file_secrets

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
)

type StoreSuite struct {
	suite.Suite
	rootDir string
	store   *Store
}

func TestStoreSuite(t *testing.T) {
	suite.Run(t, new(StoreSuite))
}

func (s *StoreSuite) SetupTest() {
	s.rootDir = s.T().TempDir()

	store, err := New(s.rootDir)
	s.NoError(err)
	s.NotNil(store)
	s.store = store
}

// --- New ---

func (s *StoreSuite) TestNew_CreatesRootDir() {
	rootDir := filepath.Join(s.rootDir, "nested", "secrets")

	store, err := New(rootDir)
	s.NoError(err)
	s.NotNil(store)

	info, err := os.Stat(rootDir)
	s.NoError(err)
	s.True(info.IsDir())
}

func (s *StoreSuite) TestNew_InvalidRootDir() {
	blocker := filepath.Join(s.rootDir, "blocker")
	s.NoError(os.WriteFile(blocker, []byte("not a dir"), 0o600))

	_, err := New(filepath.Join(blocker, "secrets"))
	s.Error(err)
}

// --- Get ---

func (s *StoreSuite) TestGet_Success() {
	err := s.store.Set(context.Background(), "/path", "name", "value")
	s.NoError(err)

	val, err := s.store.Get(context.Background(), "/path", "name")
	s.NoError(err)
	s.Equal("value", val)
}

func (s *StoreSuite) TestGet_NoLeadingSlash() {
	err := s.store.Set(context.Background(), "path", "name", "value")
	s.NoError(err)

	val, err := s.store.Get(context.Background(), "/path", "name")
	s.NoError(err)
	s.Equal("value", val)
}

func (s *StoreSuite) TestGet_NotFound() {
	val, err := s.store.Get(context.Background(), "/missing", "name")
	s.Error(err)
	s.Empty(val)
	s.Contains(err.Error(), "secret not found")
	s.Contains(err.Error(), "/missing")
	s.Contains(err.Error(), "name")
}

func (s *StoreSuite) TestGet_NotFoundPathExists() {
	err := s.store.Set(context.Background(), "/path", "other", "val")
	s.NoError(err)

	val, err := s.store.Get(context.Background(), "/path", "missing")
	s.Error(err)
	s.Empty(val)
	s.Contains(err.Error(), "secret not found")
}

func (s *StoreSuite) TestGet_ContextCancelled() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	val, err := s.store.Get(ctx, "/path", "name")
	s.Error(err)
	s.Empty(val)
	s.Equal(context.Canceled, err)
}

// --- Set ---

func (s *StoreSuite) TestSet_Success() {
	err := s.store.Set(context.Background(), "/path", "name", "value")
	s.NoError(err)

	val, err := s.store.Get(context.Background(), "/path", "name")
	s.NoError(err)
	s.Equal("value", val)
}

func (s *StoreSuite) TestSet_Upsert() {
	err := s.store.Set(context.Background(), "/path", "name", "old")
	s.NoError(err)
	err = s.store.Set(context.Background(), "/path", "name", "new")
	s.NoError(err)

	val, err := s.store.Get(context.Background(), "/path", "name")
	s.NoError(err)
	s.Equal("new", val)
}

func (s *StoreSuite) TestSet_MultiplePaths() {
	_ = s.store.Set(context.Background(), "/a", "key", "val_a")
	_ = s.store.Set(context.Background(), "/b", "key", "val_b")

	valA, _ := s.store.Get(context.Background(), "/a", "key")
	valB, _ := s.store.Get(context.Background(), "/b", "key")
	s.Equal("val_a", valA)
	s.Equal("val_b", valB)
}

func (s *StoreSuite) TestSet_ContextCancelled() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.store.Set(ctx, "/path", "name", "value")
	s.Error(err)
	s.Equal(context.Canceled, err)
}

func (s *StoreSuite) TestSet_InvalidPath() {
	for _, path := range []string{"", "/", "a/../b", "a//b", "./a", "../a", "a/.."} {
		err := s.store.Set(context.Background(), path, "name", "value")
		s.Error(err, path)
		s.Contains(err.Error(), "invalid secret path", path)
	}
}

func (s *StoreSuite) TestSet_FilePermissions() {
	err := s.store.Set(context.Background(), "/path", "name", "value")
	s.NoError(err)

	info, err := os.Stat(filepath.Join(s.rootDir, "path", "secrets.json"))
	s.NoError(err)
	s.Equal(os.FileMode(0o600), info.Mode().Perm())
}

// --- Delete ---

func (s *StoreSuite) TestDelete_Success() {
	_ = s.store.Set(context.Background(), "/path", "name", "value")

	err := s.store.Delete(context.Background(), "/path", "name")
	s.NoError(err)

	_, err = s.store.Get(context.Background(), "/path", "name")
	s.Error(err)
}

func (s *StoreSuite) TestDelete_MissingKey() {
	err := s.store.Delete(context.Background(), "/path", "missing")
	s.NoError(err)
}

func (s *StoreSuite) TestDelete_MissingPath() {
	err := s.store.Delete(context.Background(), "/missing", "name")
	s.NoError(err)
}

func (s *StoreSuite) TestDelete_KeepsOtherKeys() {
	_ = s.store.Set(context.Background(), "/path", "a", "val_a")
	_ = s.store.Set(context.Background(), "/path", "b", "val_b")

	err := s.store.Delete(context.Background(), "/path", "a")
	s.NoError(err)

	valB, err := s.store.Get(context.Background(), "/path", "b")
	s.NoError(err)
	s.Equal("val_b", valB)
}

func (s *StoreSuite) TestDelete_ContextCancelled() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.store.Delete(ctx, "/path", "name")
	s.Error(err)
	s.Equal(context.Canceled, err)
}

// --- Persistence ---

func (s *StoreSuite) TestPersistence_AcrossInstances() {
	_ = s.store.Set(context.Background(), "/linear/oauth", "org-1", "token-1")
	_ = s.store.Set(context.Background(), "/github/installations", "123", "gh-token")

	reopened, err := New(s.rootDir)
	s.NoError(err)

	val, err := reopened.Get(context.Background(), "/linear/oauth", "org-1")
	s.NoError(err)
	s.Equal("token-1", val)

	val, err = reopened.Get(context.Background(), "/github/installations", "123")
	s.NoError(err)
	s.Equal("gh-token", val)
}

func (s *StoreSuite) TestPersistence_DeletedAcrossInstances() {
	_ = s.store.Set(context.Background(), "/path", "name", "value")

	err := s.store.Delete(context.Background(), "/path", "name")
	s.NoError(err)

	reopened, err := New(s.rootDir)
	s.NoError(err)

	_, err = reopened.Get(context.Background(), "/path", "name")
	s.Error(err)
	s.Contains(err.Error(), "secret not found")
}
