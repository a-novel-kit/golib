package httpf

// The black-box tests reach these unexported functions through the names below.
var (
	NewPoolTransport = newPoolTransport
	NewServer        = newServer
	Serve            = serve
)
