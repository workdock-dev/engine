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

type EventClassificationService struct {
	GitEventFilterService
}

func (s *EventClassificationService) IsCancelSignal(signal string) bool {
	return signal == "stop"
}

func (s *EventClassificationService) IsInstallationGrant(action string) bool {
	return action == "created" || action == "added"
}

func (s *EventClassificationService) IsInstallationRevocation(action string) bool {
	return action == "removed" || action == "deleted"
}

func (s *EventClassificationService) ShouldProcessInstallation(action string) bool {
	return s.IsInstallationGrant(action) || s.IsInstallationRevocation(action)
}
