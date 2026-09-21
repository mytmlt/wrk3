//go:build windows

package runner

import (
	"bytes"
	"context"
)

// runStreaming on windows has no process-group kill (see exec_windows.go),
// so it buffers like runDockerCmd and feeds captured output to the sink
// after completion. Live progress is unavailable there; the console still
// shows the full command output, just post-hoc.
func runStreaming(ctx context.Context, name, dir string, env []string, sink OutputFunc, args ...string) (stdout, stderr string, err error) {
	var outB, errB bytes.Buffer
	err = runDockerCmd(ctx, name, dir, env, &outB, &errB, args...)
	FeedLines(sink, outB.String())
	FeedLines(sink, errB.String())
	return outB.String(), errB.String(), err
}
