// Package servicetokentest runs a Kubernetes service-account issuer for tests and local stacks. It
// serves the discovery document and key set a servicetoken.Verifier fetches, and signs the service
// tokens a test or a local caller presents. The command cmd/service-token-issuer serves it in a
// container.
//
// The issuer signs a token for anyone who asks, so only a test or a local stack may trust it.
package servicetokentest
