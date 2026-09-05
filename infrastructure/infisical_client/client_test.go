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

package infisical_client

import (
	"context"
	"fmt"
	"testing"

	sdk "github.com/infisical/go-sdk"
	"github.com/stretchr/testify/suite"
)

type InfisicalServiceSuite struct {
	suite.Suite
	service *InfisicalClient
	secrets *mockSecrets
}

func TestInfisicalServiceSuite(t *testing.T) {
	suite.Run(t, new(InfisicalServiceSuite))
}

func (s *InfisicalServiceSuite) SetupTest() {
	s.secrets = &mockSecrets{}
	s.service = &InfisicalClient{
		config: InfisicalServiceConfig{
			ClientId:     "client-id",
			ClientSecret: "client-secret",
			ProjectId:    "project-id",
			Environment:  "dev",
		},
		client: &mockInfisicalClient{
			secretsFn: func() sdk.SecretsInterface { return s.secrets },
		},
	}
}

// --- Get ---

func (s *InfisicalServiceSuite) TestGet_Success() {
	s.secrets.retrieveFn = func(opts sdk.RetrieveSecretOptions) (sdk.Secret, error) {
		return sdk.Secret{SecretValue: "my-secret"}, nil
	}
	val, err := s.service.Get(context.Background(), "/app", "db_pass")
	s.NoError(err)
	s.Equal("my-secret", val)
}

func (s *InfisicalServiceSuite) TestGet_SDKError() {
	s.secrets.retrieveFn = func(opts sdk.RetrieveSecretOptions) (sdk.Secret, error) {
		return sdk.Secret{}, fmt.Errorf("connection refused")
	}
	val, err := s.service.Get(context.Background(), "/app", "db_pass")
	s.Error(err)
	s.Empty(val)
	s.Contains(err.Error(), "connection refused")
}

func (s *InfisicalServiceSuite) TestGet_ContextCancelled() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	val, err := s.service.Get(ctx, "/app", "db_pass")
	s.Error(err)
	s.Empty(val)
	s.Equal(context.Canceled, err)
}

func (s *InfisicalServiceSuite) TestGet_CorrectOptions() {
	s.secrets.retrieveFn = func(opts sdk.RetrieveSecretOptions) (sdk.Secret, error) {
		s.Equal("db_pass", opts.SecretKey)
		s.Equal("/app", opts.SecretPath)
		s.Equal("project-id", opts.ProjectID)
		s.Equal("dev", opts.Environment)
		return sdk.Secret{SecretValue: "val"}, nil
	}
	_, err := s.service.Get(context.Background(), "/app", "db_pass")
	s.NoError(err)
}

// --- Set: create path (retrieve → 404 → create) ---

func (s *InfisicalServiceSuite) TestSet_Create() {
	s.secrets.retrieveFn = func(opts sdk.RetrieveSecretOptions) (sdk.Secret, error) {
		return sdk.Secret{}, &sdk.APIError{StatusCode: 404}
	}
	s.secrets.createFn = func(opts sdk.CreateSecretOptions) (sdk.Secret, error) {
		s.Equal("db_pass", opts.SecretKey)
		s.Equal("new-value", opts.SecretValue)
		s.Equal("/app", opts.SecretPath)
		s.Equal("project-id", opts.ProjectID)
		s.Equal("dev", opts.Environment)
		return sdk.Secret{SecretKey: "db_pass", SecretValue: "new-value"}, nil
	}
	err := s.service.Set(context.Background(), "/app", "db_pass", "new-value")
	s.NoError(err)
}

// --- Set: update path (retrieve → success → update) ---

func (s *InfisicalServiceSuite) TestSet_Update() {
	s.secrets.retrieveFn = func(opts sdk.RetrieveSecretOptions) (sdk.Secret, error) {
		return sdk.Secret{SecretValue: "old"}, nil
	}
	s.secrets.updateFn = func(opts sdk.UpdateSecretOptions) (sdk.Secret, error) {
		s.Equal("db_pass", opts.SecretKey)
		s.Equal("new-value", opts.NewSecretValue)
		s.Equal("/app", opts.SecretPath)
		s.Equal("project-id", opts.ProjectID)
		s.Equal("dev", opts.Environment)
		return sdk.Secret{}, nil
	}
	err := s.service.Set(context.Background(), "/app", "db_pass", "new-value")
	s.NoError(err)
}

func (s *InfisicalServiceSuite) TestSet_RetrieveError() {
	s.secrets.retrieveFn = func(opts sdk.RetrieveSecretOptions) (sdk.Secret, error) {
		return sdk.Secret{}, fmt.Errorf("connection refused")
	}
	err := s.service.Set(context.Background(), "/app", "db_pass", "new-value")
	s.Error(err)
	s.Contains(err.Error(), "failed to check secret db_pass")
	s.Contains(err.Error(), "connection refused")
}

func (s *InfisicalServiceSuite) TestSet_CreateError() {
	s.secrets.retrieveFn = func(opts sdk.RetrieveSecretOptions) (sdk.Secret, error) {
		return sdk.Secret{}, &sdk.APIError{StatusCode: 404}
	}
	s.secrets.createFn = func(opts sdk.CreateSecretOptions) (sdk.Secret, error) {
		return sdk.Secret{}, fmt.Errorf("create failed")
	}
	err := s.service.Set(context.Background(), "/app", "db_pass", "val")
	s.Error(err)
	s.Contains(err.Error(), "create failed")
}

func (s *InfisicalServiceSuite) TestSet_UpdateError() {
	s.secrets.retrieveFn = func(opts sdk.RetrieveSecretOptions) (sdk.Secret, error) {
		return sdk.Secret{SecretValue: "old"}, nil
	}
	s.secrets.updateFn = func(opts sdk.UpdateSecretOptions) (sdk.Secret, error) {
		return sdk.Secret{}, fmt.Errorf("update failed")
	}
	err := s.service.Set(context.Background(), "/app", "db_pass", "val")
	s.Error(err)
	s.Contains(err.Error(), "failed to update secret db_pass")
	s.Contains(err.Error(), "update failed")
}

func (s *InfisicalServiceSuite) TestSet_ContextCancelled() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.service.Set(ctx, "/app", "db_pass", "val")
	s.Error(err)
	s.Equal(context.Canceled, err)
}

func (s *InfisicalServiceSuite) TestSet_RetrieveCalledBeforeCreate() {
	var callOrder []string
	s.secrets.retrieveFn = func(opts sdk.RetrieveSecretOptions) (sdk.Secret, error) {
		callOrder = append(callOrder, "retrieve")
		return sdk.Secret{}, &sdk.APIError{StatusCode: 404}
	}
	s.secrets.createFn = func(opts sdk.CreateSecretOptions) (sdk.Secret, error) {
		callOrder = append(callOrder, "create")
		return sdk.Secret{}, nil
	}
	err := s.service.Set(context.Background(), "/app", "key", "val")
	s.NoError(err)
	s.Equal([]string{"retrieve", "create"}, callOrder)
}

func (s *InfisicalServiceSuite) TestSet_RetrieveCalledBeforeUpdate() {
	var callOrder []string
	s.secrets.retrieveFn = func(opts sdk.RetrieveSecretOptions) (sdk.Secret, error) {
		callOrder = append(callOrder, "retrieve")
		return sdk.Secret{SecretValue: "old"}, nil
	}
	s.secrets.updateFn = func(opts sdk.UpdateSecretOptions) (sdk.Secret, error) {
		callOrder = append(callOrder, "update")
		return sdk.Secret{}, nil
	}
	err := s.service.Set(context.Background(), "/app", "key", "val")
	s.NoError(err)
	s.Equal([]string{"retrieve", "update"}, callOrder)
}

// --- Delete ---

func (s *InfisicalServiceSuite) TestDelete_Success() {
	s.secrets.deleteFn = func(opts sdk.DeleteSecretOptions) (sdk.Secret, error) {
		s.Equal("db_pass", opts.SecretKey)
		s.Equal("/app", opts.SecretPath)
		s.Equal("project-id", opts.ProjectID)
		s.Equal("dev", opts.Environment)
		return sdk.Secret{}, nil
	}
	err := s.service.Delete(context.Background(), "/app", "db_pass")
	s.NoError(err)
}

func (s *InfisicalServiceSuite) TestDelete_NotFound() {
	s.secrets.deleteFn = func(opts sdk.DeleteSecretOptions) (sdk.Secret, error) {
		return sdk.Secret{}, &sdk.APIError{StatusCode: 404}
	}
	err := s.service.Delete(context.Background(), "/app", "db_pass")
	s.NoError(err)
}

func (s *InfisicalServiceSuite) TestDelete_Error() {
	s.secrets.deleteFn = func(opts sdk.DeleteSecretOptions) (sdk.Secret, error) {
		return sdk.Secret{}, fmt.Errorf("permission denied")
	}
	err := s.service.Delete(context.Background(), "/app", "db_pass")
	s.Error(err)
	s.Contains(err.Error(), "permission denied")
}

func (s *InfisicalServiceSuite) TestDelete_ContextCancelled() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.service.Delete(ctx, "/app", "db_pass")
	s.Error(err)
	s.Equal(context.Canceled, err)
}
