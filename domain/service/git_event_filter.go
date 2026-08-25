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

type GitEventFilterService struct{}

func (f *GitEventFilterService) ShouldTriggerCommentEvent(senderLogin, botLoginName string, action string) bool {
	if senderLogin == botLoginName {
		return false
	}
	if action == "deleted" {
		return false
	}
	return true
}

func (f *GitEventFilterService) ShouldTriggerCheckRunEvent(senderLogin, botLoginName, action, conclusion string) bool {
	if senderLogin == botLoginName {
		return false
	}
	if action != "completed" {
		return false
	}
	if conclusion == "" {
		return false
	}
	if conclusion != "failure" && conclusion != "timed_out" {
		return false
	}
	return true
}
