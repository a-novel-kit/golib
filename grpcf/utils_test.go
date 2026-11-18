package grpcf_test

import (
	"os"
	"testing"

	"github.com/a-novel-kit/golib/servicetoken"
	"github.com/a-novel-kit/golib/servicetoken/servicetokentest"
)

const (
	serviceTokenAudience = "json-keys"
	serviceTokenCaller   = "system:serviceaccount:authentication:api"
)

// serviceTokenConfig admits serviceTokenCaller to serviceTokenAudience on the tokens of issuer.
func serviceTokenConfig(issuer string) servicetoken.Config {
	return servicetoken.Config{Issuer: issuer, Audience: serviceTokenAudience, Callers: []string{serviceTokenCaller}}
}

// signServiceToken returns a token of issuer that subject holds for serviceTokenAudience.
func signServiceToken(t *testing.T, issuer *servicetokentest.Issuer, subject string) string {
	t.Helper()

	return issuer.Sign(t, issuer.Claims(subject, serviceTokenAudience))
}

// writeServiceToken projects token into the file at path, as the kubelet does.
func writeServiceToken(t *testing.T, path, token string) {
	t.Helper()

	err := os.WriteFile(path, []byte(token), 0o600)
	if err != nil {
		panic(err)
	}
}
