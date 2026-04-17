// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

//go:build !windows

package transport

import (
	"fmt"
	"math"
	"syscall"
)

// reuseControl sets SO_REUSEADDR and SO_REUSEPORT on the socket.
func reuseControl(network, address string, c syscall.RawConn) error {
	var errSet error
	controlErr := c.Control(func(fd uintptr) {
		if fd > uintptr(math.MaxInt) {
			errSet = fmt.Errorf("socket descriptor overflows int: %d", fd)
			return
		}
		errSet = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	})
	if controlErr != nil {
		return controlErr
	}
	return errSet
}
