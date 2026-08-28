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

package factories

import (
	"testing"
)

func TestIdempotencyKeyFactory_Build(t *testing.T) {
	factory := &IdempotencyKeyFactory{}

	input := IdempotencyKeyInput{
		ID:        "test-id-123",
		Timestamp: "2026-08-28T00:00:00Z",
		Seed:      ptr("test-seed"),
	}

	key1, err := factory.Build(input)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if key1 == "" {
		t.Fatal("Expected non-empty idempotency key")
	}

	key2, err := factory.Build(input)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if key1 != key2 {
		t.Errorf("Expected same key for same input, got %q and %q", key1, key2)
	}

	input2 := IdempotencyKeyInput{
		ID:        "test-id-123",
		Timestamp: "2026-08-28T00:00:01Z",
		Seed:      ptr("test-seed"),
	}

	key3, err := factory.Build(input2)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if key1 == key3 {
		t.Error("Expected different keys for different timestamps")
	}
}

func TestIdempotencyKeyFactory_Build_NilSeed(t *testing.T) {
	factory := &IdempotencyKeyFactory{}

	input := IdempotencyKeyInput{
		ID:        "test-id-123",
		Timestamp: "2026-08-28T00:00:00Z",
		Seed:      nil,
	}

	key, err := factory.Build(input)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if key == "" {
		t.Fatal("Expected non-empty idempotency key")
	}
}

func ptr(s string) *string {
	return &s
}
