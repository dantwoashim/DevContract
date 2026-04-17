// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package transport

import "syscall"

// reuseControl sets SO_REUSEADDR on Windows.
// Windows does not support SO_REUSEPORT.
func reuseControl(network, address string, c syscall.RawConn) error {
	var errSet error
	controlErr := c.Control(func(fd uintptr) {
		errSet = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	})
	if controlErr != nil {
		return controlErr
	}
	return errSet
}
