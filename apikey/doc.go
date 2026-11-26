// Package apikey authenticates requests signed with an API key.
//
// An API key is an Ed25519 key pair made for one purpose, such as publish-narrative-engine-plugin.
// Its owner holds the private key and never sends it. For each request the owner signs a request
// token with it: a JWT that names the purpose, lives five minutes at most and carries the SHA-256 of
// the request's body. A request token therefore authenticates the one request it was signed for.
//
// A [Verifier] checks a request token against the public key of a system API key, a key of the
// platform itself, which a service reads from a setting named for the purpose. httpf holds the
// transport glue.
package apikey
