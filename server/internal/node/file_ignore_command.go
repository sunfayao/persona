package node

import (
	"context"
	"os/exec"
	"time"
)

func fileIgnoreCommand(ctx context.Context, dir string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "-c", "core.quotepath=off", "--no-pager", "check-ignore", "-z", "--stdin")
	cmd.Env = append(cmd.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	cmd.WaitDelay = time.Second
	return cmd
}
