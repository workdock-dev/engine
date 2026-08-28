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

import (
	"errors"
	"io"
	"net/textproto"
	"time"
)

// WebhookRequest carries the raw incoming webhook payload to a provider
// adapter for validation and parsing. It deliberately avoids net/http types:
// the HTTP edge converts the request into this neutral shape so ports and the
// domain never depend on the transport.
type WebhookRequest struct {
	// Headers holds the request headers. http.Header is defined as
	// map[string][]string, so converting at the edge is a direct assignment.
	// Keys are not guaranteed to be canonical; use Get instead of indexing
	// the map directly.
	Headers map[string][]string

	// RemoteAddr is the client network address (IP, port). It is a plain
	// string, not an HTTP type.
	RemoteAddr string

	// Body is the raw webhook payload.
	Body io.Reader
}

// Get returns the first value for the given header key, or "" when absent.
// It is case insensitive: the key is canonicalized with
// [textproto.CanonicalMIMEHeaderKey] before lookup, matching http.Header.Get.
func (r WebhookRequest) Get(key string) string {
	if values, ok := r.Headers[textproto.CanonicalMIMEHeaderKey(key)]; ok && len(values) > 0 {
		return values[0]
	}

	return ""
}

const WebhookFreshnessWindow = 60 * time.Second

var ErrWebhookStale = errors.New("webhook timestamp is outside the freshness window")

func ValidateWebhookFreshness(payloadTimestamp time.Time, now time.Time) error {
	diff := now.Sub(payloadTimestamp)

	if diff < -WebhookFreshnessWindow || diff > WebhookFreshnessWindow {
		return ErrWebhookStale
	}

	return nil
}
