package cli

import "os"

// exit is a seam over os.Exit so future tests can stub process exit
// without driving real `go test` termination. Production callers go
// through it; tests construct commands via NewRootCmd directly and
// inspect returned errors instead.
var exit = func(code int) { os.Exit(code) }
