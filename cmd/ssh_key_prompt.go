// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/dantwoashim/devcontract/internal/crypto"
	"golang.org/x/term"
)

// #nosec G101 -- This is the name of an environment variable, not a credential.
const sshKeyPassphraseEnv = "DEVCONTRACT_SSH_KEY_PASSPHRASE"

func loadSSHKeyWithPrompt(path string) (*crypto.KeyPair, error) {
	kp, err := crypto.LoadSSHKey(path)
	if err == nil {
		return kp, nil
	}
	if !errors.Is(err, crypto.ErrPassphraseRequired) && !errors.Is(err, crypto.ErrInvalidPassphrase) {
		return nil, err
	}

	for attempts := 0; attempts < 3; attempts++ {
		passphrase, fromEnv, promptErr := resolveSSHPassphrase(path, attempts > 0)
		if promptErr != nil {
			return nil, promptErr
		}

		kp, err = crypto.LoadSSHKeyWithPassphrase(path, passphrase)
		zeroBytes(passphrase)
		if err == nil {
			return kp, nil
		}
		if !errors.Is(err, crypto.ErrInvalidPassphrase) {
			return nil, err
		}
		if fromEnv {
			return nil, fmt.Errorf("%w\n\n  %s is set, but the value was not accepted for %s", err, sshKeyPassphraseEnv, path)
		}
	}

	return nil, fmt.Errorf("SSH key passphrase was not accepted after 3 attempts")
}

func resolveSSHPassphrase(path string, retry bool) ([]byte, bool, error) {
	if value, ok := os.LookupEnv(sshKeyPassphraseEnv); ok && value != "" {
		return []byte(value), true, nil
	}

	// #nosec G115 -- os.Stdin file descriptors are process-owned terminal handles.
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, false, fmt.Errorf("SSH key at %s is passphrase-protected\n\n  Set %s or rerun in an interactive terminal", path, sshKeyPassphraseEnv)
	}

	if retry {
		fmt.Fprintln(os.Stderr, "  ! Passphrase was not accepted. Try again.")
	}
	fmt.Fprintf(os.Stderr, "  - Enter SSH key passphrase for %s: ", path)
	passphrase, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, false, fmt.Errorf("reading SSH key passphrase: %w", err)
	}
	if len(passphrase) == 0 {
		return nil, false, fmt.Errorf("empty passphrase entered for %s", path)
	}
	return passphrase, false, nil
}

func zeroBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
