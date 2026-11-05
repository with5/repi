//go:build !windows

package tts

import "os/exec"

func hideWindow(*exec.Cmd) {}
