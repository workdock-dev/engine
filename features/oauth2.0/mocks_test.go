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

package oauth20

import (
	"context"
)

type mockOauthHandler struct {
	getAuthorizationURLFn func() string
	callbackFn            func(ctx context.Context, code, errCode string) (*CallbackResult, error)

	getAuthorizationURLCalled int
	callbackCalled            int
	callbackCtx               context.Context
	callbackCode              string
	callbackErrCode           string
}

func (m *mockOauthHandler) GetAuthorizationURL() string {
	m.getAuthorizationURLCalled++
	if m.getAuthorizationURLFn != nil {
		return m.getAuthorizationURLFn()
	}
	return ""
}

func (m *mockOauthHandler) Callback(ctx context.Context, code, errCode string) (*CallbackResult, error) {
	m.callbackCalled++
	m.callbackCtx = ctx
	m.callbackCode = code
	m.callbackErrCode = errCode
	if m.callbackFn != nil {
		return m.callbackFn(ctx, code, errCode)
	}
	return &CallbackResult{}, nil
}

type mockSecretManager struct {
	setFn func(ctx context.Context, secretPath, secretName, secretValue string) error

	setCalled  int
	setCtx     context.Context
	setPath    string
	setName    string
	setValue   string
}

func (m *mockSecretManager) Get(ctx context.Context, secretPath, secretName string) (string, error) {
	panic("unexpected call to Get")
}

func (m *mockSecretManager) Set(ctx context.Context, secretPath, secretName, secretValue string) error {
	m.setCalled++
	m.setCtx = ctx
	m.setPath = secretPath
	m.setName = secretName
	m.setValue = secretValue
	if m.setFn != nil {
		return m.setFn(ctx, secretPath, secretName, secretValue)
	}
	return nil
}

func (m *mockSecretManager) Delete(ctx context.Context, secretPath, secretName string) error {
	panic("unexpected call to Delete")
}