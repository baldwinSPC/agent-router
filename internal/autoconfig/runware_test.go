// Copyright Envoy AI Gateway Authors
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package autoconfig

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/yaml"

	internaltesting "github.com/envoyproxy/ai-gateway/internal/testing"
)

func TestRunwareCompatibleConfiguration(t *testing.T) {
	internaltesting.ClearTestEnv(t)
	t.Setenv("OPENAI_ORG_ID", "")
	t.Setenv("OPENAI_PROJECT_ID", "")
	t.Setenv("OPENAI_API_KEY", "not-a-real-runware-key")
	t.Setenv("OPENAI_BASE_URL", "https://api.runware.ai/v1")
	var data ConfigData
	require.NoError(t, PopulateOpenAIEnvConfig(&data))
	require.Equal(t, []Backend{{Name: "openai", Hostname: "api.runware.ai", Port: 443, NeedsTLS: true}}, data.Backends)
	require.Equal(t, "OpenAI", data.OpenAI.SchemaName)

	config, err := WriteConfig(&data)
	require.NoError(t, err)
	// Secret substitution is deferred; generated manifests must not embed the caller's credential.
	require.NotContains(t, config, "not-a-real-runware-key")
	decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(config), 4096)
	resources := map[string]map[string]any{}
	for {
		var resource map[string]any
		err = decoder.Decode(&resource)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		if resource["kind"] != nil {
			resources[resource["kind"].(string)] = resource
		}
	}
	backend := resources["Backend"]["spec"].(map[string]any)
	endpoint := backend["endpoints"].([]any)[0].(map[string]any)["fqdn"].(map[string]any)
	require.Equal(t, "api.runware.ai", endpoint["hostname"])
	require.EqualValues(t, 443, endpoint["port"])
	validation := resources["BackendTLSPolicy"]["spec"].(map[string]any)["validation"].(map[string]any)
	require.Equal(t, "api.runware.ai", validation["hostname"])
	require.Equal(t, "System", validation["wellKnownCACertificates"])
	secret := resources["Secret"]["stringData"].(map[string]any)
	require.Equal(t, "${OPENAI_API_KEY}", secret["apiKey"])
	policy := resources["BackendSecurityPolicy"]["spec"].(map[string]any)
	require.Equal(t, "APIKey", policy["type"])
	require.Equal(t, "openai-apikey", policy["apiKey"].(map[string]any)["secretRef"].(map[string]any)["name"])
}

func TestRunwareCompatibleConfigurationMissingCredential(t *testing.T) {
	internaltesting.ClearTestEnv(t)
	t.Setenv("OPENAI_BASE_URL", "https://api.runware.ai/v1")
	var data ConfigData
	require.ErrorContains(t, PopulateOpenAIEnvConfig(&data), "API_KEY")
	require.Empty(t, data.Backends)
	require.Nil(t, data.OpenAI)
}

func TestRunwareCompatibleConfigurationRejectsInvalidURL(t *testing.T) {
	for _, baseURL := range []string{"api.runware.ai/v1", "ftp://api.runware.ai/v1"} {
		t.Run(baseURL, func(t *testing.T) {
			internaltesting.ClearTestEnv(t)
			t.Setenv("OPENAI_API_KEY", "not-a-real-runware-key")
			t.Setenv("OPENAI_BASE_URL", baseURL)
			var data ConfigData
			require.ErrorContains(t, PopulateOpenAIEnvConfig(&data), "invalid base URL")
			require.Empty(t, data.Backends)
		})
	}
}

func TestRunwareCompatibleConfigurationAzurePrecedence(t *testing.T) {
	internaltesting.ClearTestEnv(t)
	t.Setenv("OPENAI_API_KEY", "not-a-real-runware-key")
	t.Setenv("OPENAI_BASE_URL", "https://api.runware.ai/v1")
	t.Setenv("AZURE_OPENAI_API_KEY", "not-a-real-azure-key")
	t.Setenv("AZURE_OPENAI_ENDPOINT", "")
	var data ConfigData
	require.ErrorContains(t, PopulateOpenAIEnvConfig(&data), "AZURE_OPENAI_ENDPOINT environment variable is required")
	require.Empty(t, data.Backends)
}
