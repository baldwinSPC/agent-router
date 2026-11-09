// Copyright Envoy AI Gateway Authors
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package runware

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/envoyproxy/ai-gateway/tests/internal/dataplaneenv"
	"github.com/envoyproxy/ai-gateway/tests/internal/testrunware"
)

//go:embed envoy.yaml
var envoyConfig string

//go:embed extproc.yaml
var extprocConfig string

func TestRunwareMockGateway(t *testing.T) {
	upstream := &testrunware.Handler{}
	env := dataplaneenv.StartTestEnvironment(t, func(t testing.TB, _ io.Writer, ports map[string]int) {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", ports["runware"]))
		require.NoError(t, err)
		server := &http.Server{Handler: upstream, ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = server.Serve(listener) }()
		t.Cleanup(func() { require.NoError(t, server.Close()) })
	}, map[string]int{"runware": 11434}, extprocConfig, nil, envoyConfig, true, false, 30*time.Second)
	client := &http.Client{Timeout: 10 * time.Second}
	for _, responses := range []bool{false, true} {
		family, path, alias, model := "chat", "/v1/chat/completions", "runware-chat", testrunware.ChatModel
		if responses {
			family, path, alias, model = "responses", "/v1/responses", "runware-responses", testrunware.ResponsesModel
		}
		for _, tc := range []struct {
			name, scenario, alias, input, errorCode      string
			status                                       int
			stream, invalid, notForwarded, responsesOnly bool
		}{
			{name: "json", status: 200},
			{name: "stream", status: 200, stream: true},
			{name: "wrong_backend_key", alias: "runware-wrong-key", status: 401, errorCode: "invalid_api_key"},
			{name: "invalid_upstream_model", alias: "runware-invalid-model", status: 404, errorCode: "model_not_found"},
			{name: "unknown_route", alias: "not-configured", status: 404, notForwarded: true},
			{name: "upstream_error", scenario: "upstream-error", status: 503, errorCode: "synthetic_unavailable"},
			{name: "rate_limit", scenario: "rate-limit", status: 429, errorCode: "rate_limit_exceeded"},
			{name: "bad_json", scenario: "bad-json", status: 500, invalid: true},
			{name: "broken_stream", scenario: "broken-stream", status: 200, stream: true, invalid: true},
			{name: "error_stream", scenario: "error-stream", status: 200, stream: true, invalid: true},
			{name: "stateful_responses", responsesOnly: true, input: `{"model":"runware-responses","input":"OK","previous_response_id":"resp_prior"}`, status: 400, errorCode: "unsupported_previous_response_id"},
			{name: "malformed_request", input: `{"model":`, status: 400, notForwarded: true},
		} {
			if tc.responsesOnly && !responses {
				continue
			}
			t.Run(family+"_"+tc.name, func(t *testing.T) {
				requestAlias := alias
				if tc.alias != "" {
					requestAlias = tc.alias
				}
				body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"Reply OK"}],"stream":%t,"stream_options":{"include_usage":true}}`, requestAlias, tc.stream)
				if responses {
					body = fmt.Sprintf(`{"model":%q,"input":"Reply OK","stream":%t}`, requestAlias, tc.stream)
				}
				if tc.input != "" {
					body = tc.input
				}
				before := upstream.Requests.Load()
				request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d%s", env.EnvoyListenerPort(), path), bytes.NewBufferString(body))
				require.NoError(t, err)
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Authorization", "Bearer client-credential-must-be-replaced")
				request.Header.Set("x-ai-eg-model", "runware-chat")
				request.Header.Set(testrunware.ScenarioHeader, tc.scenario)
				response, err := client.Do(request)
				require.NoError(t, err)
				defer response.Body.Close()
				result, readErr := io.ReadAll(response.Body)
				if !tc.invalid {
					require.NoError(t, readErr)
				}
				require.Equal(t, tc.status, response.StatusCode, "body: %s", result)
				require.NotContains(t, string(result), testrunware.APIKey)
				require.NotContains(t, string(result), "client-credential-must-be-replaced")
				if tc.notForwarded {
					require.Equal(t, before, upstream.Requests.Load(), "unroutable or malformed request reached upstream")
					return
				}
				require.Equal(t, before+1, upstream.Requests.Load(), "request was retried or not forwarded")
				if tc.errorCode != "" {
					require.Contains(t, string(result), tc.errorCode)
					if tc.status == 429 {
						require.Equal(t, "2", response.Header.Get("Retry-After"))
					}
					return
				}
				receipt := upstream.LastRequest()
				require.Equal(t, path, receipt.Path)
				require.Equal(t, model, receipt.Model, "model alias was not replaced")
				require.True(t, receipt.Authenticated, "configured backend credential did not replace client credential")
				validation := testrunware.ValidateCompletion(responses, tc.stream, result)
				if tc.invalid {
					require.Error(t, validation, "invalid body was accepted as a completed response")
					return
				}
				require.NoError(t, validation)
				if tc.stream {
					require.Contains(t, response.Header.Get("Content-Type"), "text/event-stream")
				}
			})
		}
	}
}
