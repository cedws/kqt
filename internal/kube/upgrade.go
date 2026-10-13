package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"time"

	gwebsocket "github.com/gorilla/websocket"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	clientwebsocket "k8s.io/client-go/transport/websocket"
	streamhttp "k8s.io/streaming/pkg/httpstream"
	"k8s.io/streaming/pkg/httpstream/wsstream"
)

// Gorilla's handshake read honors deadlines, but does not close an established
// TCP socket when a deadline-free context is canceled. Keep a cancellation hook
// only until the HTTP upgrade completes. Thereafter client-go owns the stream.
type cancellableUpgrade struct {
	holder *clientwebsocket.RoundTripper
	dial   func(context.Context, string, string) (net.Conn, error)
}

func (t *cancellableUpgrade) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Body != nil {
		defer request.Body.Close()
	}
	protocols := request.Header[wsstream.WebSocketProtocolHeader]
	headers := request.Header.Clone()
	headers.Del(wsstream.WebSocketProtocolHeader)
	url := *request.URL
	switch url.Scheme {
	case "https":
		url.Scheme = "wss"
	case "http":
		url.Scheme = "ws"
	default:
		return nil, fmt.Errorf("unsupported WebSocket URL scheme %q", url.Scheme)
	}
	dial := t.dial
	if dial == nil {
		dial = (&net.Dialer{Timeout: 15 * time.Second}).DialContext
	}
	var stopCancel func() bool
	defer func() {
		if stopCancel != nil {
			stopCancel()
		}
	}()
	connector := gwebsocket.Dialer{Proxy: t.holder.Proxier, TLSClientConfig: t.holder.TLSConfig, Subprotocols: protocols, ReadBufferSize: t.holder.DataBufferSize() + 1024, WriteBufferSize: t.holder.DataBufferSize() + 1024, HandshakeTimeout: 15 * time.Second,
		NetDialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			connection, err := dial(ctx, network, address)
			if err != nil {
				return nil, err
			}
			// Watch the caller context, not the dialer's temporary handshake context:
			// the latter is automatically canceled even after a successful upgrade.
			stopCancel = context.AfterFunc(request.Context(), func() { connection.Close() })
			return connection, nil
		}}
	connection, response, err := connector.DialContext(request.Context(), url.String(), headers)
	if err != nil {
		if request.Context().Err() != nil {
			return nil, request.Context().Err()
		}
		if !errors.Is(err, gwebsocket.ErrBadHandshake) {
			return nil, err
		}
		cause := error(err)
		if response != nil {
			defer response.Body.Close()
			data, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
			var status metav1.Status
			if json.Unmarshal(data, &status) == nil && status.Kind == "Status" {
				cause = apierrors.FromObject(&status)
			} else {
				cause = fmt.Errorf("WebSocket upgrade rejected: %s", response.Status)
			}
		}
		return nil, &streamhttp.UpgradeFailureError{Cause: cause}
	}
	if !slices.Contains(protocols, connection.Subprotocol()) {
		connection.Close()
		return nil, &streamhttp.UpgradeFailureError{Cause: fmt.Errorf("server selected an unsupported streaming protocol")}
	}
	t.holder.Conn = connection
	return response, nil
}

func (c *Client) streamingConfig(ctx context.Context) *rest.Config {
	cfg := rest.CopyConfig(c.config)
	cfg.Timeout = 0
	original, dial := cfg.WrapTransport, cfg.Dial
	cfg.WrapTransport = func(base http.RoundTripper) http.RoundTripper {
		if holder, ok := base.(*clientwebsocket.RoundTripper); ok {
			base = &cancellableUpgrade{holder: holder, dial: dial}
		}
		if original != nil {
			base = original(base)
		}
		return discoveryTransport{base: base, ctx: ctx}
	}
	return cfg
}
