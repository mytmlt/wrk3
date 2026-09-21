//go:build unix

package runner

import (
	"bufio"
	"context"
	"os/exec"
	"sync"
	"syscall"
)

// runStreaming runs name args... with cwd=dir and env in its own process
// group, teeing every stdout/stderr line to sink live (see startKillable
// for why the group kill matters: the compose plugin forks grandchildren
// that hold the captured pipes). Combined output is still returned for
// error text, matching runDockerCmd.
func runStreaming(ctx context.Context, name, dir string, env []string, sink OutputFunc, args ...string) (stdout, stderr string, err error) {
	c := exec.Command(name, args...)
	c.Dir = dir
	c.Env = env
	stdoutPipe, err := c.StdoutPipe()
	if err != nil {
		return "", "", err
	}
	stderrPipe, err := c.StderrPipe()
	if err != nil {
		return "", "", err
	}
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := c.Start(); err != nil {
		return "", "", err
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		case <-done:
		}
	}()

	var mu sync.Mutex
	var outBuf, errBuf []byte
	tee := func(pipe interface {
		Read([]byte) (int, error)
	}, buf *[]byte) {
		sc := bufio.NewScanner(pipe)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			mu.Lock()
			*buf = append(*buf, line...)
			*buf = append(*buf, '\n')
			mu.Unlock()
			FeedLine(sink, line)
		}
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); tee(stdoutPipe, &outBuf) }()
	go func() { defer wg.Done(); tee(stderrPipe, &errBuf) }()
	waitErr := c.Wait()
	wg.Wait()
	return string(outBuf), string(errBuf), waitErr
}
