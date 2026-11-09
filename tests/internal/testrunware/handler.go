// Copyright Envoy AI Gateway Authors
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package testrunware serves synthetic compatible text responses. It has no
// recording mode, credential lookup, outbound client, or live-provider fallback.
package testrunware

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/envoyproxy/ai-gateway/internal/json"
)

const (
	APIKey         = "runware-mock-key"
	ChatModel      = "mock:chat@1"
	ResponsesModel = "mock:responses@1"
	ScenarioHeader = "X-Runware-Mock-Scenario"
)

type Receipt struct {
	Path          string
	Model         string
	Authenticated bool
}

type Handler struct {
	Requests atomic.Int64
	mu       sync.Mutex
	last     Receipt
}

func (h *Handler) LastRequest() Receipt {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.last
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.Requests.Add(1)
	receipt := Receipt{Path: r.URL.Path, Authenticated: r.Header.Get("Authorization") == "Bearer "+APIKey}
	defer func() {
		h.mu.Lock()
		h.last = receipt
		h.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method_not_allowed")
		return
	}
	if !receipt.Authenticated {
		writeError(w, http.StatusUnauthorized, "authentication_error", "invalid_api_key")
		return
	}
	var request struct {
		Model              string            `json:"model"`
		Stream             bool              `json:"stream"`
		Input              json.RawMessage   `json:"input"`
		Messages           []json.RawMessage `json:"messages"`
		PreviousResponseID string            `json:"previous_response_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "invalid_json")
		return
	}
	receipt.Model = request.Model
	responses := r.URL.Path == "/v1/responses"
	if !responses && r.URL.Path != "/v1/chat/completions" {
		writeError(w, http.StatusNotFound, "invalid_request_error", "unknown_endpoint")
		return
	}
	model := ChatModel
	if responses {
		model = ResponsesModel
	}
	if request.Model != model {
		writeError(w, http.StatusNotFound, "invalid_request_error", "model_not_found")
		return
	}
	if request.PreviousResponseID != "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "unsupported_previous_response_id")
		return
	}
	if (responses && len(request.Input) == 0) || (!responses && len(request.Messages) == 0) {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "missing_input")
		return
	}
	scenario := r.Header.Get(ScenarioHeader)
	switch scenario {
	case "rate-limit":
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusTooManyRequests, "rate_limit_error", "rate_limit_exceeded")
		return
	case "upstream-error":
		writeError(w, http.StatusServiceUnavailable, "server_error", "synthetic_unavailable")
		return
	case "bad-json":
		_, _ = fmt.Fprint(w, `{"id":`)
		return
	}
	if !request.Stream {
		writeJSON(w, response(responses, model))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	if responses {
		writeEvent(w, "response.created", map[string]any{"type": "response.created", "sequence_number": 0, "response": map[string]any{"id": "resp_synthetic", "object": "response", "status": "in_progress", "model": model}})
		writeEvent(w, "response.output_text.delta", map[string]any{"type": "response.output_text.delta", "sequence_number": 1, "item_id": "msg_synthetic", "output_index": 0, "content_index": 0, "delta": "OK"})
		if scenario == "broken-stream" {
			return
		}
		if scenario == "error-stream" {
			writeEvent(w, "response.failed", map[string]any{"type": "response.failed", "sequence_number": 2, "response": map[string]any{"id": "resp_synthetic", "status": "failed", "error": map[string]any{"code": "synthetic_failure", "message": "Synthetic stream failure"}}})
			return
		}
		writeEvent(w, "response.completed", map[string]any{"type": "response.completed", "sequence_number": 2, "response": response(true, model)})
		return
	}
	writeEvent(w, "", map[string]any{"id": "chatcmpl_synthetic", "object": "chat.completion.chunk", "created": 1700000000, "model": model, "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": "OK"}, "finish_reason": nil}}})
	if scenario == "broken-stream" {
		return
	}
	if scenario == "error-stream" {
		writeEvent(w, "", map[string]any{"error": map[string]any{"type": "server_error", "code": "synthetic_failure", "message": "Synthetic stream failure"}})
		return
	}
	writeEvent(w, "", map[string]any{"id": "chatcmpl_synthetic", "object": "chat.completion.chunk", "created": 1700000000, "model": model, "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}})
	writeEvent(w, "", map[string]any{"id": "chatcmpl_synthetic", "object": "chat.completion.chunk", "created": 1700000000, "model": model, "choices": []any{}, "usage": map[string]any{"prompt_tokens": 4, "completion_tokens": 1, "total_tokens": 5}})
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flush(w)
}

func response(responses bool, model string) map[string]any {
	if responses {
		return map[string]any{"id": "resp_synthetic", "object": "response", "created_at": 1700000000, "status": "completed", "model": model, "error": nil, "output": []any{map[string]any{"id": "msg_synthetic", "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "OK", "annotations": []any{}}}}}, "usage": map[string]any{"input_tokens": 4, "output_tokens": 1, "total_tokens": 5}}
	}
	return map[string]any{"id": "chatcmpl_synthetic", "object": "chat.completion", "created": 1700000000, "model": model, "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "OK"}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 4, "completion_tokens": 1, "total_tokens": 5}}
}

func writeError(w http.ResponseWriter, status int, kind, code string) {
	w.WriteHeader(status)
	writeJSON(w, map[string]any{"error": map[string]any{"type": kind, "code": code, "message": "Synthetic Runware-compatible failure", "param": nil}})
}

func writeJSON(w http.ResponseWriter, value any) { _ = json.NewEncoder(w).Encode(value) }

func writeEvent(w http.ResponseWriter, event string, value any) {
	if event != "" {
		_, _ = fmt.Fprintf(w, "event: %s\n", event)
	}
	data, _ := json.Marshal(value)
	line := "data: " + string(data) + "\n\n"
	// Flush in the middle of JSON to exercise consumers across transport fragments.
	half := len(line) / 2
	_, _ = fmt.Fprint(w, line[:half])
	flush(w)
	_, _ = fmt.Fprint(w, line[half:])
	flush(w)
}

func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// ValidateCompletion rejects HTTP-200 bodies that do not establish a complete
// successful synthetic response. It is a test/client assertion, not a gateway filter.
func ValidateCompletion(responses, streaming bool, body []byte) error {
	if !streaming {
		var value map[string]any
		if err := json.Unmarshal(body, &value); err != nil {
			return err
		}
		return validateJSON(responses, value)
	}
	var content, terminal, usage, finished bool
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			terminal = true
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return err
		}
		if event["error"] != nil || event["type"] == "response.failed" || event["type"] == "response.incomplete" {
			return fmt.Errorf("stream failed")
		}
		if responses {
			if event["type"] == "response.output_text.delta" && event["delta"] == "OK" {
				content = true
			}
			if event["type"] == "response.completed" {
				value, ok := event["response"].(map[string]any)
				if !ok {
					return fmt.Errorf("missing terminal response")
				}
				if err := validateJSON(true, value); err != nil {
					return err
				}
				terminal, usage = true, true
			}
		} else {
			if event["model"] != ChatModel {
				return fmt.Errorf("wrong streaming model")
			}
			if choices, ok := event["choices"].([]any); ok && len(choices) > 0 {
				choice, _ := choices[0].(map[string]any)
				if choice["finish_reason"] == "stop" {
					finished = true
				}
				if delta, ok := choice["delta"].(map[string]any); ok && delta["content"] == "OK" {
					content = true
				}
			}
			if value, ok := event["usage"].(map[string]any); ok && value["total_tokens"] == float64(5) {
				usage = true
			}
		}
	}
	if !content || !terminal || !usage || (!responses && !finished) {
		return fmt.Errorf("missing stream content, terminal event or supplied usage")
	}
	return nil
}

func validateJSON(responses bool, value map[string]any) error {
	if value["error"] != nil {
		return fmt.Errorf("response error")
	}
	usage, hasUsage := value["usage"].(map[string]any)
	if !hasUsage || usage["total_tokens"] != float64(5) {
		return fmt.Errorf("missing or incorrect supplied usage")
	}
	if responses {
		if usage["input_tokens"] != float64(4) || usage["output_tokens"] != float64(1) {
			return fmt.Errorf("wrong supplied Responses usage")
		}
		if value["status"] != "completed" || value["model"] != ResponsesModel {
			return fmt.Errorf("wrong Responses status or model")
		}
		output, ok := value["output"].([]any)
		if !ok || len(output) == 0 {
			return fmt.Errorf("missing output")
		}
		item, _ := output[0].(map[string]any)
		content, _ := item["content"].([]any)
		if len(content) == 0 {
			return fmt.Errorf("missing output content")
		}
		text, _ := content[0].(map[string]any)
		if text["text"] != "OK" {
			return fmt.Errorf("wrong output content")
		}
		return nil
	}
	if usage["prompt_tokens"] != float64(4) || usage["completion_tokens"] != float64(1) {
		return fmt.Errorf("wrong supplied chat usage")
	}
	if value["model"] != ChatModel {
		return fmt.Errorf("wrong chat model")
	}
	choices, ok := value["choices"].([]any)
	if !ok || len(choices) == 0 {
		return fmt.Errorf("missing choices")
	}
	choice, _ := choices[0].(map[string]any)
	message, _ := choice["message"].(map[string]any)
	if message["content"] != "OK" || choice["finish_reason"] != "stop" {
		return fmt.Errorf("incomplete chat message")
	}
	return nil
}
