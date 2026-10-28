package servicetoken

import (
	"fmt"
	"os"
	"strings"
)

// Header is the request header, or gRPC metadata key, that carries the service token of a call. It
// leaves the Authorization header to the credentials of an end user.
const Header = "X-Service-Authorization"

// Read returns the service token in the file at path, where a projected volume mounts it. The
// kubelet replaces the file before the token expires, so a caller reads it again for each call.
func Read(path string) (string, error) {
	token, err := os.ReadFile(path) //nolint:gosec // The path comes from the service's configuration.
	if err != nil {
		return "", fmt.Errorf("read service token: %w", err)
	}

	return strings.TrimSpace(string(token)), nil
}
