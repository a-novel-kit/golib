package otelpresets

import (
	"fmt"
	stdlog "log"
	"os"
)

// The bold ANSI colors of the startup banners: bright green for a local run, bright blue for an
// export to a backend.
const (
	bannerStart  = "\x1b[1;92m"
	bannerExport = "\x1b[1;94m"
)

// printBanner prints the startup banner text to standard output, in color.
func printBanner(color, text string) {
	_, _ = fmt.Fprintln(os.Stdout, color+text+"\x1b[0m")
}

// trimLogFlags drops the date and time from the standard logger's lines, for a process whose logs a
// terminal shows.
func trimLogFlags() {
	stdlog.SetFlags(stdlog.Flags() &^ (stdlog.Ldate | stdlog.Ltime))
}
