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
	"errors"
	"fmt"
	"log/slog"

	sdk "github.com/infisical/go-sdk"
)

const (
	SiteUrl = "https://app.infisical.com"
)

type InfisicalServiceConfig struct {
	ClientId     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	ProjectId    string `yaml:"project_id"`
	Environment  string `yaml:"environment"`
	SiteURL      string `yaml:"site_url"`
}

type InfisicalClient struct {
	config InfisicalServiceConfig
	client sdk.InfisicalClientInterface
}

// New initializes the Infisical client and establishes an
// authenticated session for secret management.
//
//   - Configures the Infisical client using the provided environment and project
//     settings.
//   - Authenticates using Universal Auth to enable secure access to the project's
//     secrets.
//   - Enables automatic token refresh so the client can maintain authentication
//     without manual intervention.
//
// Service initialization fails if authentication with Infisical cannot be
// established.
func New(ctx context.Context, config InfisicalServiceConfig) (*InfisicalClient, error) {
	if config.SiteURL == "" {
		config.SiteURL = SiteUrl
	}

	client := sdk.NewInfisicalClient(ctx, sdk.Config{
		SiteUrl:          config.SiteURL,
		AutoTokenRefresh: new(true),
	})

	_, err := client.Auth().UniversalAuthLogin(config.ClientId, config.ClientSecret)

	if err != nil {
		slog.Error("[secret-manager][infisical] failed to authenticate using universal auth",
			"err", err,
			"clientId", config.ClientId,
			"env", config.Environment,
			"projectId", config.ProjectId,
			"siteUrl", config.SiteURL,
		)
		return nil, err
	}

	slog.Debug("[secret-manager][infisical] client created")
	return &InfisicalClient{
		client: client,
		config: config,
	}, nil
}

// Get retrieves a secret from Infisical.
//
//   - Looks up a secret by its path and name within the configured project and
//     environment.
//   - Returns the current value of the secret for use by application components.
//
// The caller is responsible for interpreting the secret's contents.
func (s *InfisicalClient) Get(ctx context.Context, secretPath, secretName string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	secret, err := s.client.Secrets().Retrieve(sdk.RetrieveSecretOptions{
		SecretKey:   secretName,
		SecretPath:  secretPath,
		ProjectID:   s.config.ProjectId,
		Environment: s.config.Environment,
	})

	if err != nil {
		slog.Error("[secret-manager][infisical] failed to retrieve secret", "secretName", secretName, "secretPath", secretPath, "err", err)
		return "", err
	}

	return secret.SecretValue, nil
}

// Set creates or updates a secret in Infisical.
//
//   - Ensures the specified secret exists with the provided value in the
//     configured project and environment.
//   - Creates the secret if it does not already exist.
//   - Updates the existing secret when it has already been provisioned.
//
// After a successful call, the stored secret reflects the provided value
// regardless of whether it was newly created or updated.
func (s *InfisicalClient) Set(ctx context.Context, secretPath, secretName, secretValue string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	_, retrieveErr := s.client.Secrets().Retrieve(sdk.RetrieveSecretOptions{
		SecretKey:   secretName,
		SecretPath:  secretPath,
		ProjectID:   s.config.ProjectId,
		Environment: s.config.Environment,
	})

	if retrieveErr != nil {
		var apiErr *sdk.APIError

		if errors.As(retrieveErr, &apiErr) && apiErr.StatusCode == 404 {
			_, err := s.client.Secrets().Create(sdk.CreateSecretOptions{
				SecretKey:   secretName,
				SecretValue: secretValue,
				SecretPath:  secretPath,
				ProjectID:   s.config.ProjectId,
				Environment: s.config.Environment,
			})

			if err != nil {
				slog.Error("[secret-manager][infisical] failed to create secret", "secretName", secretName, "secretPath", secretPath, "err", err)
				return err
			}

			return nil
		}

		err := fmt.Errorf("failed to check secret %s: %w", secretName, retrieveErr)
		slog.Error("[secret-manager][infisical] failed to set secret", "secretName", secretName, "secretPath", secretPath, "err", err)
		return err
	}

	_, err := s.client.Secrets().Update(sdk.UpdateSecretOptions{
		SecretKey:      secretName,
		NewSecretValue: secretValue,
		SecretPath:     secretPath,
		ProjectID:      s.config.ProjectId,
		Environment:    s.config.Environment,
	})

	if err != nil {
		return fmt.Errorf("[secret-manager][infisical] failed to update secret %s: %w", secretName, err)
	}

	return nil
}

// Delete removes a secret from Infisical.
//
//   - Deletes the secret by its path and name within the configured project
//     and environment.
//   - Treats a missing secret as already deleted (no error).
func (s *InfisicalClient) Delete(ctx context.Context, secretPath, secretName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	_, err := s.client.Secrets().Delete(sdk.DeleteSecretOptions{
		SecretKey:   secretName,
		SecretPath:  secretPath,
		ProjectID:   s.config.ProjectId,
		Environment: s.config.Environment,
	})

	if err != nil {
		var apiErr *sdk.APIError

		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			return nil
		}

		slog.Error("[secret-manager][infisical] failed to delete secret", "secretName", secretName, "secretPath", secretPath, "err", err)
		return err
	}

	return nil
}
