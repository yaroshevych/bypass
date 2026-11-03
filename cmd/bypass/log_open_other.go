//go:build !darwin && !linux

package main

import (
	"fmt"
	"os"
)

// Refuse this optional feature on platforms without the supported no-follow open.
func openDiagnosticLog(path string) (*os.File, error) {
	return nil, fmt.Errorf("diagnostic logs are not supported on this platform")
}
