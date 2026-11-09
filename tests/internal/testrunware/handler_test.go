// Copyright Envoy AI Gateway Authors
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package testrunware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompletionAssertionsRejectInvalidBodies(t *testing.T) {
	for _, tc := range []struct {
		name              string
		responses, stream bool
		body              string
	}{
		{name: "chat_error_on_200", body: `{"error":{"code":"synthetic_failure"}}`},
		{name: "responses_error_on_200", responses: true, body: `{"error":{"code":"synthetic_failure"}}`},
		{name: "malformed_json", body: `{"id":`},
		{name: "pending_responses", responses: true, body: `{"status":"in_progress","model":"mock:responses@1","output":[],"usage":{"input_tokens":4,"output_tokens":1,"total_tokens":5}}`},
		{name: "chat_missing_terminal", stream: true, body: "data: {\"model\":\"mock:chat@1\",\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\n"},
		{name: "responses_missing_terminal", responses: true, stream: true, body: "data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\n"},
		{name: "stream_error", responses: true, stream: true, body: "data: {\"type\":\"response.failed\"}\n\n"},
		{name: "bad_stream_json", stream: true, body: "data: {broken\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Error(t, ValidateCompletion(tc.responses, tc.stream, []byte(tc.body))) })
	}
}

func TestSyntheticBackendRejectsStatefulResponses(t *testing.T) {
	handler := &Handler{}
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/v1/responses", strings.NewReader(`{"model":"mock:responses@1","input":"OK","previous_response_id":"resp_prior"}`))
	r.Header.Set("Authorization", "Bearer "+APIKey)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "unsupported_previous_response_id")
}
