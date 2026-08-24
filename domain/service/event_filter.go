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

type EventFilterService struct{}

func NewEventFilterService() *EventFilterService {
	return &EventFilterService{}
}

type PullRequestCommentFilterInput struct {
	BotLoginName string
	SenderLogin  string
	Action       string
}

type CheckRunFilterInput struct {
	BotLoginName   string
	SenderLogin    string
	Action         string
	Conclusion     string
}

func (s *EventFilterService) ShouldTriggerSessionReRunForComment(input PullRequestCommentFilterInput) bool {
	if input.SenderLogin == input.BotLoginName {
		return false
	}

	if input.Action == "deleted" {
		return false
	}

	return true
}

func (s *EventFilterService) ShouldTriggerSessionReRunForCheckRun(input CheckRunFilterInput) bool {
	if input.SenderLogin == input.BotLoginName {
		return false
	}

	if input.Action != "completed" {
		return false
	}

	if input.Conclusion != "failure" && input.Conclusion != "timed_out" {
		return false
	}

	return true
}
