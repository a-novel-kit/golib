// Package servertls serves TLS from a key pair renewed on disk, such as a certificate cert-manager
// keeps current in a Kubernetes Secret the kubelet mounts.
//
// httpf and grpcf build their servers' TLS from it, so a service serves TLS through its
// configuration alone.
package servertls
