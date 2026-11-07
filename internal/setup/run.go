package setup

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// runner runs an external command, passing each non-empty output line
// (stdout and stderr) to onLine.
type runner interface {
	Run(ctx context.Context, env []string, onLine func(string), name string, args ...string) error
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, env []string, onLine func(string), name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	cmd.WaitDelay = 5 * time.Second
	hideWindow(cmd)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return err
	}
	var last []string // the last lines, for the error message
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			if l == "" {
				continue
			}
			if last = append(last, l); len(last) > 4 {
				last = last[1:]
			}
			onLine(l)
		}
		io.Copy(io.Discard, pr) // a line too long for the scanner
	}()
	err := cmd.Wait()
	pw.Close()
	<-done
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("%w：%s", err, strings.Join(last, " / "))
	}
	return nil
}
