// SPDX-License-Identifier: GPL-3.0-only

//go:build !linux

package mobile

import "os"

// Вне Linux (тесты на рабочей станции) работа с дескриптором TUN не поддерживается.
func tunnelName(int32) (string, error) { return "", os.ErrInvalid }

func dupFd(int32) (int, error) { return 0, os.ErrInvalid }
