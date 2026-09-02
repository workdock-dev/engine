package webhook

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/textproto"
)

var (
	ErrWBadRequest          = errors.New("webhook bad request")
	ErrWForBidden           = errors.New("webhook forbidden")
	ErrWUnAuthorized        = errors.New("webhook unauthorized")
	ErrWServerInternalError = errors.New("server internal error")
)

// WebhookRequest carries the raw incoming webhook payload to a provider
// adapter for validation and parsing. It deliberately avoids net/http types:
// the HTTP edge converts the request into this neutral shape so ports and the
// domain never depend on the transport.
type WEvent struct {
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
func (r WEvent) Get(key string) string {
	if values, ok := r.Headers[textproto.CanonicalMIMEHeaderKey(key)]; ok && len(values) > 0 {
		return values[0]
	}

	return ""
}

// VerifiedWEvent contains a webhook that has passed provider-specific
// request verification.
type VerifiedWEvent struct {
	WEventType string
	DeliveryID string // varies by provider
	Payload    []byte
}

// WEventTransformer converts an HTTP request into a transport-neutral webhook
// event.
type WEventTransformer interface {
	Transform(ctx context.Context, r *http.Request) (*WEvent, error)
}

// WEventVerifier validates a webhook event and produces a verified event.
type WEventVerifier interface {
	Verify(ctx context.Context, event *WEvent) (*VerifiedWEvent, error)
}

// WEventConsumer processes a verified webhook event.
type WEventConsumer interface {
	Consume(ctx context.Context, event *VerifiedWEvent) error
}

// controller executes the webhook processing pipeline.
type controller struct {
	transformer WEventTransformer
	verifier    WEventVerifier
	consumer    WEventConsumer
	mux         *http.ServeMux
	endpoint    string
}

// New creates a webhook runner from the components that make up
// the transformation, verification, and consumption stages.
func New(
	endpoint string,
	mux *http.ServeMux,
	transformer WEventTransformer,
	verifier WEventVerifier,
	consumer WEventConsumer,
) {
	c := &controller{
		transformer: transformer,
		verifier:    verifier,
		consumer:    consumer,
		mux:         mux,
		endpoint:    endpoint,
	}
	c.init()
}

func (c *controller) init() {
	c.mux.HandleFunc(c.endpoint, func(w http.ResponseWriter, req *http.Request) {
		if err := c.execute(req); err != nil {
			status := http.StatusInternalServerError

			if errors.Is(err, ErrWBadRequest) {
				status = http.StatusBadRequest
			}

			if errors.Is(err, ErrWUnAuthorized) {
				status = http.StatusUnauthorized
			}

			if errors.Is(err, ErrWForBidden) {
				status = http.StatusForbidden
			}

			w.WriteHeader(status)
			return
		}

		w.WriteHeader(http.StatusAccepted)
	})
}

// Execute processes an HTTP webhook request through the pipeline.
//
// The pipeline terminates immediately when transformation or verification
// returns an error. The consumer is only invoked for successfully verified
// events, and any consumer error is returned to the caller.
func (c *controller) execute(req *http.Request) error {
	ctx := req.Context()
	wevent, err := c.transformer.Transform(ctx, req)

	if err != nil {
		slog.Error("failed to transform request to wevent", "err", err)
		return err
	}

	result, err := c.verifier.Verify(ctx, wevent)

	if err != nil {
		slog.Error("failed verified wevent", "err", err)
		return err
	}

	if err := c.consumer.Consume(ctx, result); err != nil {
		slog.Error("Failed to consume wevent", "err", err)
		return err
	}

	return nil
}
