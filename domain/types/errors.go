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

package types

import "errors"

var (
	ErrBadRequest          = errors.New("400 BadRequest")
	ErrUnAuthorized        = errors.New("401 UnAuthorized")
	ErrForbidden           = errors.New("403 Forbidden")
	ErrInternalServerError = errors.New("500 Internal Server Errro")

	// ErrLinearTokenExpired is returned when the Linear access token has
	// expired (or expires imminently) but no refresh token is available,
	// requiring the user to re-authorize the application.
	ErrLinearTokenExpired = errors.New("linear access token expired and no refresh token is available")

	// ErrLinearTokenRefreshFailed is returned when renewing an expired Linear
	// access token fails, requiring the user to take action.
	ErrLinearTokenRefreshFailed = errors.New("failed to refresh linear access token")

	// ErrGitHubInstallationUnavailable is returned when a GitHub App
	// installation can no longer be reached (e.g. it was removed or belongs
	// to a different app), so its credentials cannot be renewed.
	ErrGitHubInstallationUnavailable = errors.New("github installation unavailable")

	// ErrGitHubConnectionReRequested is returned when a GitHub installation is
	// no longer available: the installation has been reset and a fresh
	// connection requested, so the user should be prompted to re-authorize.
	ErrGitHubConnectionReRequested = errors.New("github connection re-requested")

	// ErrHarnessUnhealthy is returned when a harness's liveness probe
	// determines the agent process has stopped emitting output and must be
	// disposed so the job can be retried.
	ErrHarnessUnhealthy = errors.New("harness declared unhealthy")
)
