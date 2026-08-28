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

package domain_service

import "github.com/workdock-dev/engine/domain/types"

type IssueLifecycleService struct{}

func NewIssueLifecycleService() *IssueLifecycleService {
	return &IssueLifecycleService{}
}

func (s *IssueLifecycleService) ShouldArchiveForIssue(stateType string) bool {
	return stateType == string(types.IssueStateCompleted)
}
