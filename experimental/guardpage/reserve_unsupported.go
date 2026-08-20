//go:build !((darwin || linux) && (amd64 || arm64))

package guardpage

import "errors"

// Supported is false on this platform.
const Supported = false

const reserveSize = 0

var errUnsupported = errors.New("guardpage: not supported on this platform")

func reserve(uintptr) ([]byte, error) { return nil, errUnsupported }
func commit([]byte, uintptr) error    { return errUnsupported }
func release([]byte) error            { return errUnsupported }
