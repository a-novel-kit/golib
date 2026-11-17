package servicetoken_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/servicetoken"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		env map[string]string

		expect servicetoken.Config
	}{
		{
			name: "Success/Unset",
		},
		{
			name: "Success/Set",

			//nolint:gosec // Names of public identities, not credentials.
			env: map[string]string{
				"SERVICE_TOKEN_ISSUER":   "https://issuer.example",
				"SERVICE_TOKEN_AUDIENCE": "json-keys",
				"SERVICE_TOKEN_CALLERS":  "system:serviceaccount:a:web, system:serviceaccount:b:api",
			},

			expect: servicetoken.Config{
				Issuer:   "https://issuer.example",
				Audience: "json-keys",
				Callers:  []string{"system:serviceaccount:a:web", "system:serviceaccount:b:api"},
			},
		},
		{
			name: "Success/IssuerOptional",

			env: map[string]string{"SERVICE_ISSUER_OPTIONAL": "true"},

			expect: servicetoken.Config{IssuerOptional: true},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config := servicetoken.LoadConfig(func(name string) string { return testCase.env[name] })
			require.Equal(t, testCase.expect, config)
		})
	}
}
