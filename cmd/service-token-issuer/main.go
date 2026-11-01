// Command service-token-issuer stands in for a Kubernetes cluster's service-account issuer in a
// local stack, such as a compose file. It serves a [servicetokentest.Issuer] on port 8080, at the
// URL SERVICE_TOKEN_ISSUER names. Each service that verifies the stack's tokens sets its own
// SERVICE_TOKEN_ISSUER to the same URL, such as http://service-token-issuer:8080.
//
// The issuer signs a token for anyone who asks, so only a local stack may trust it. A restart draws
// a new key, and the tokens signed before it fail once a verifier refreshes its keys.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/a-novel-kit/golib/servicetoken/servicetokentest"
)

func main() {
	url := os.Getenv("SERVICE_TOKEN_ISSUER")
	if url == "" {
		log.Fatal("SERVICE_TOKEN_ISSUER is required")
	}

	issuer, err := servicetokentest.NewIssuerAt(url)
	if err != nil {
		log.Fatal(err)
	}

	server := &http.Server{Addr: ":8080", Handler: issuer, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}
