# Runware compatible text example

This development scaffold routes Chat Completions and Responses through the existing
OpenAI backend schema. The default example uses a loopback-only synthetic backend.
It never loads a Runware key or forwards requests to a live provider.

## Run the local acceptance demo

From the repository root, with Go and make installed:

```sh
make test-runware
```

This starts Envoy, the external processor and a synthetic backend on ephemeral
loopback ports, exercises both endpoints in JSON and streaming modes, and stops
the processes automatically. Envoy and Go dependencies may be downloaded on the
first run. No Kubernetes cluster, account balance or API key is needed.

Tests check model aliases, backend credential replacement, supplied usage,
fragmented SSE, auth errors, invalid models, unknown routes, rate limits and
upstream failures. Malformed upstream JSON returns HTTP 500. Truncated streams and stream error
events must fail the client-side completion assertions even if HTTP 200 was sent. An HTTP 200 alone does not
establish successful completion, and this scaffold does not add a gateway filter
that changes that behavior. Fixtures are synthetic, not Runware recordings.

## Explore interactively

In terminal one, from the repository root:

```sh
make run-runware-mock
```

In terminal two:

```sh
make run-runware-local
```

In terminal three:

```sh
curl --fail-with-body http://127.0.0.1:1062/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"runware-chat","messages":[{"role":"user","content":"Reply OK"}]}'

curl --fail-with-body --no-buffer http://127.0.0.1:1062/v1/responses \
  -H 'Content-Type: application/json' \
  -d '{"model":"runware-responses","input":"Reply OK","stream":true}'
```

Add `"stream":true,"stream_options":{"include_usage":true}` to the chat request
to inspect chat streaming. Remove `"stream":true` from Responses for JSON.
Stop terminals one and two with Ctrl-C. Ports 1062, 1064 and 11434 must be free.
The local CLI uses `local.yaml`, including a public dummy credential and synthetic
model IDs. Never put a real credential in this file.

For synthetic failure injection, add `X-Runware-Mock-Scenario: rate-limit`,
`upstream-error`, `bad-json`, `broken-stream` or `error-stream` to a request.
These headers belong to this local mock only. An unconfigured model has no route.

## Prepare a live deployment

[`../basic/runware.yaml`](../basic/runware.yaml) is the separate Kubernetes
configuration for `api.runware.ai:443`, using TLS validation and an existing
`runware-apikey` Secret in the `default` namespace. It assumes the Gateway from
`../basic/basic.yaml` and the required Gateway/AI Gateway CRDs are installed.
It does not contain or create a real credential.

Do not apply or smoke-test that manifest until funding, the test budget and model
IDs are approved. Its documented candidate IDs are `minimax:m2.7@0` for the
`runware-chat` alias and `openai:gpt@5.4-mini` for `runware-responses`; edit the
model overrides to the approved AIR IDs. Account availability is unverified.

Runware Responses is stateless: send history in `input`; `previous_response_id`
is unsupported. Native image tasks and Anthropic Messages are outside this slice.
See the [compatible API contract](https://runware.ai/docs/tools/compatible-apis).

Once approved, create the Secret through your normal secret-management workflow,
then apply the manifest and use the same client aliases. A funded top-up is not
automatically a hard spending cap. Live evidence must be recorded separately
from the synthetic tests before claiming Runware support.
