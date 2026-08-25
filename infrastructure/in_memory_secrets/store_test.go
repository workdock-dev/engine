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

package in_memory_secrets

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
)

type StoreSuite struct {
	suite.Suite
	store *Store
}

func TestStoreSuite(t *testing.T) {
	suite.Run(t, new(StoreSuite))
}

func (s *StoreSuite) SetupTest() {
	s.store = New()
}

// --- New ---

func (s *StoreSuite) TestNew() {
	store := New()
	s.NotNil(store)
	s.NotNil(store.secrets)
	s.Empty(store.secrets)
}

// --- NewWithSeeds ---

func (s *StoreSuite) TestNewWithSeeds() {
	seeds := map[string]map[string]string{
		"/app/prod": {"db_pass": "s3cret", "api_key": "key123"},
		"/app/dev":  {"db_pass": "dev123"},
	}
	store := NewWithSeeds(seeds)
	s.NotNil(store)

	val, err := store.Get(context.Background(), "/app/prod", "db_pass")
	s.NoError(err)
	s.Equal("s3cret", val)

	val, err = store.Get(context.Background(), "/app/prod", "api_key")
	s.NoError(err)
	s.Equal("key123", val)

	val, err = store.Get(context.Background(), "/app/dev", "db_pass")
	s.NoError(err)
	s.Equal("dev123", val)
}

func (s *StoreSuite) TestNewWithSeeds_Empty() {
	store := NewWithSeeds(map[string]map[string]string{})
	s.NotNil(store)
	s.Empty(store.secrets)
}

func (s *StoreSuite) TestNewWithSeeds_NilInnerMap() {
	seeds := map[string]map[string]string{
		"/empty": nil,
	}
	store := NewWithSeeds(seeds)
	s.NotNil(store)
	s.NotNil(store.secrets["/empty"])
}

// --- Get ---

func (s *StoreSuite) TestGet_Success() {
	err := s.store.Set(context.Background(), "/path", "name", "value")
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

func (s *StoreSuite) TestDelete_ContextCancelled() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.store.Delete(ctx, "/path", "name")
	s.Error(err)
	s.Equal(context.Canceled, err)
}
