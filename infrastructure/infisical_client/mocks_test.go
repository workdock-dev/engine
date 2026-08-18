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
	sdk "github.com/infisical/go-sdk"
	"github.com/infisical/go-sdk/packages/models"
)

// --- mockInfisicalClient ---

type mockInfisicalClient struct {
	secretsFn func() sdk.SecretsInterface
	authFn    func() sdk.AuthInterface
}

func (m *mockInfisicalClient) UpdateConfiguration(config sdk.Config) {}
func (m *mockInfisicalClient) Folders() sdk.FoldersInterface         { panic("unexpected call") }
func (m *mockInfisicalClient) DynamicSecrets() sdk.DynamicSecretsInterface {
	panic("unexpected call")
}
func (m *mockInfisicalClient) Kms() sdk.KmsInterface  { panic("unexpected call") }
func (m *mockInfisicalClient) Ssh() sdk.SshInterface  { panic("unexpected call") }

func (m *mockInfisicalClient) Secrets() sdk.SecretsInterface {
	if m.secretsFn != nil {
		return m.secretsFn()
	}
	return &mockSecrets{}
}

func (m *mockInfisicalClient) Auth() sdk.AuthInterface {
	if m.authFn != nil {
		return m.authFn()
	}
	return &mockAuth{}
}

// --- mockSecrets ---

type mockSecrets struct {
	retrieveFn func(options sdk.RetrieveSecretOptions) (models.Secret, error)
	createFn   func(options sdk.CreateSecretOptions) (models.Secret, error)
	updateFn   func(options sdk.UpdateSecretOptions) (models.Secret, error)
	deleteFn   func(options sdk.DeleteSecretOptions) (models.Secret, error)
}

func (m *mockSecrets) List(options sdk.ListSecretsOptions) ([]models.Secret, error) {
	panic("unexpected call")
}
func (m *mockSecrets) ListSecrets(options sdk.ListSecretsOptions) (sdk.ListSecretsResult, error) {
	panic("unexpected call")
}
func (m *mockSecrets) Batch() sdk.BatchSecretsInterface { panic("unexpected call") }

func (m *mockSecrets) Retrieve(options sdk.RetrieveSecretOptions) (models.Secret, error) {
	if m.retrieveFn != nil {
		return m.retrieveFn(options)
	}
	return models.Secret{}, nil
}

func (m *mockSecrets) Create(options sdk.CreateSecretOptions) (models.Secret, error) {
	if m.createFn != nil {
		return m.createFn(options)
	}
	return models.Secret{SecretKey: options.SecretKey, SecretValue: options.SecretValue}, nil
}

func (m *mockSecrets) Update(options sdk.UpdateSecretOptions) (models.Secret, error) {
	if m.updateFn != nil {
		return m.updateFn(options)
	}
	return models.Secret{}, nil
}

func (m *mockSecrets) Delete(options sdk.DeleteSecretOptions) (models.Secret, error) {
	if m.deleteFn != nil {
		return m.deleteFn(options)
	}
	return models.Secret{}, nil
}

// --- mockAuth ---

type mockAuth struct {
	universalAuthLoginFn func(clientID string, clientSecret string) (sdk.MachineIdentityCredential, error)
}

func (m *mockAuth) SetAccessToken(accessToken string)                       {}
func (m *mockAuth) GetAccessToken() string                                  { return "" }
func (m *mockAuth) GetOrganizationSlug() string                             { return "" }
func (m *mockAuth) WithOrganizationSlug(slug string) sdk.AuthInterface      { return m }
func (m *mockAuth) WithAzureClientID(clientID string) sdk.AuthInterface     { return m }
func (m *mockAuth) JwtAuthLogin(string, string) (sdk.MachineIdentityCredential, error) {
	panic("unexpected call")
}
func (m *mockAuth) KubernetesAuthLogin(string, string) (sdk.MachineIdentityCredential, error) {
	panic("unexpected call")
}
func (m *mockAuth) KubernetesRawServiceAccountTokenLogin(string, string) (sdk.MachineIdentityCredential, error) {
	panic("unexpected call")
}
func (m *mockAuth) AzureAuthLogin(string, string) (sdk.MachineIdentityCredential, error) {
	panic("unexpected call")
}
func (m *mockAuth) GcpIdTokenAuthLogin(string) (sdk.MachineIdentityCredential, error) {
	panic("unexpected call")
}
func (m *mockAuth) GcpIamAuthLogin(string, string) (sdk.MachineIdentityCredential, error) {
	panic("unexpected call")
}
func (m *mockAuth) AwsIamAuthLogin(string) (sdk.MachineIdentityCredential, error) {
	panic("unexpected call")
}
func (m *mockAuth) OidcAuthLogin(string, string) (sdk.MachineIdentityCredential, error) {
	panic("unexpected call")
}
func (m *mockAuth) OciAuthLogin(sdk.OciAuthLoginOptions) (sdk.MachineIdentityCredential, error) {
	panic("unexpected call")
}
func (m *mockAuth) LdapAuthLogin(string, string, string) (sdk.MachineIdentityCredential, error) {
	panic("unexpected call")
}
func (m *mockAuth) RevokeAccessToken() error { return nil }

func (m *mockAuth) UniversalAuthLogin(clientID string, clientSecret string) (sdk.MachineIdentityCredential, error) {
	if m.universalAuthLoginFn != nil {
		return m.universalAuthLoginFn(clientID, clientSecret)
	}
	return sdk.MachineIdentityCredential{}, nil
}
