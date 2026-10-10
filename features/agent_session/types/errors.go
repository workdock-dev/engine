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

// ExecutionError keeps user-facing copy separate from the underlying diagnostics.
type ExecutionError struct {
	Message string
	Cause   error
}

func (e *ExecutionError) Error() string {
	return e.Cause.Error()
}

func (e *ExecutionError) Unwrap() error {
	return e.Cause
}

func WithExecutionMessage(err error, message string) error {
	if err == nil {
		return nil
	}

	var executionError *ExecutionError
	if errors.As(err, &executionError) {
		return err
	}

	return &ExecutionError{Message: message, Cause: err}
}
