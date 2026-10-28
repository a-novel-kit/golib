// Package servicetoken authenticates calls between services with service tokens.
//
// A service token is a Kubernetes service-account token bound to the audience of the service it
// calls. The kubelet projects it into the caller's pod as a file and replaces it before it expires.
// The cluster's OpenID Connect issuer signs every token and publishes its public keys.
//
// The caller reads its token with [Read] for each call and sends it in the [Header] header. The
// callee checks it with a [Verifier] against the issuer's keys, which the Verifier caches, so a call
// costs no request to the issuer. grpcf and httpf hold the transport glue on both sides.
package servicetoken
