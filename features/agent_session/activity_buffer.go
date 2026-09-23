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

package agent_session

import (
	"context"
	"strings"
)

const activityBufferFlushBytes = 4 * 1024

// activityBuffer coalesces adjacent stream fragments. Structured activities
// flush it first so their order remains visible to the user.
type activityBuffer struct {
	kind         string
	body         strings.Builder
	sendThought  func(context.Context, string) error
	sendResponse func(context.Context, string) error
}

func newActivityBuffer(
	sendThought func(context.Context, string) error,
	sendResponse func(context.Context, string) error,
) *activityBuffer {
	return &activityBuffer{
		sendThought:  sendThought,
		sendResponse: sendResponse,
	}
}

func (b *activityBuffer) Thought(ctx context.Context, text string) error {
	return b.append(ctx, "thought", text)
}

func (b *activityBuffer) Response(ctx context.Context, text string) error {
	return b.append(ctx, "response", text)
}

func (b *activityBuffer) append(ctx context.Context, kind, text string) error {
	if text == "" {
		return nil
	}

	if b.kind != "" && b.kind != kind {
		if err := b.Flush(ctx); err != nil {
			return err
		}
	}

	b.kind = kind
	b.body.WriteString(text)

	if b.body.Len() >= activityBufferFlushBytes {
		return b.Flush(ctx)
	}

	return nil
}

func (b *activityBuffer) Flush(ctx context.Context) error {
	if b.kind == "" || b.body.Len() == 0 {
		return nil
	}

	kind, body := b.kind, b.body.String()
	b.kind = ""
	b.body.Reset()

	if kind == "thought" {
		return b.sendThought(ctx, body)
	}

	return b.sendResponse(ctx, body)
}
