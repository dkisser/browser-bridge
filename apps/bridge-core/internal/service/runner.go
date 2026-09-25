package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// Runner abstracts process inspection, signalling, and launchctl — every
// touchpoint that would otherwise mutate the developer's real launchd or
// processes. The real implementation is ExecRunner; unit tests inject a
// fake. Defined here (consumer side) per the project's interface rules.
type Runner interface {
	// LaunchctlLoaded reports whether the launchd job label is loaded.
	LaunchctlLoaded(ctx context.Context, label string) (bool, error)
	// LaunchctlBootstrap loads the plist into the domain (e.g. gui/501).
	LaunchctlBootstrap(ctx context.Context, domainTarget, plistPath string) error
	// LaunchctlBootout unloads the job; a job that is not loaded is success.
	LaunchctlBootout(ctx context.Context, domainTarget, label string) error
	// PS returns the process state ("R", "Ss", "Z", ...) and the full
	// command line. The error is non-nil when the pid is gone or invisible.
	PS(ctx context.Context, pid int) (state, command string, err error)
	// Kill sends sig to pid (sig 0 is the existence probe).
	Kill(pid int, sig syscall.Signal) error
	// UID is the caller's user id, used for launchd domain targets.
	UID() int
}

// ExecRunner is the production Runner: real launchctl, ps, and signals.
type ExecRunner struct{}

// LaunchctlLoaded mirrors `launchctl list | awk '{print $3}' | grep -qx`.
func (ExecRunner) LaunchctlLoaded(ctx context.Context, label string) (bool, error) {
	out, err := exec.CommandContext(ctx, "launchctl", "list").Output()
	if err != nil {
		return false, fmt.Errorf("launchctl list: %w", err)
	}
	for line := range strings.Lines(string(out)) {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[2] == label {
			return true, nil
		}
	}
	return false, nil
}

// LaunchctlBootstrap runs `launchctl bootstrap <domainTarget> <plist>`.
func (ExecRunner) LaunchctlBootstrap(ctx context.Context, domainTarget, plistPath string) error {
	cmd := exec.CommandContext(ctx, "launchctl", "bootstrap", domainTarget, plistPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launchctl bootstrap %s %s: %w (%s)", domainTarget, plistPath, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// LaunchctlBootout mirrors launchagent_bootout: gui domain first, then the
// user domain for jobs older releases bootstrapped there. Only the last
// attempt's error surfaces, matching the bash `||` chain.
func (r ExecRunner) LaunchctlBootout(ctx context.Context, domainTarget, label string) error {
	if err := exec.CommandContext(ctx, "launchctl", "bootout", domainTarget+"/"+label).Run(); err == nil {
		return nil
	}
	userTarget := "user/" + strconv.Itoa(r.UID())
	cmd := exec.CommandContext(ctx, "launchctl", "bootout", userTarget+"/"+label)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launchctl bootout %s/%s: %w (%s)", userTarget, label, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// PS mirrors `ps -p <pid> -o state= -o command=`; the bash pid_is matched
// against comm+args combined, which the command column covers.
func (ExecRunner) PS(ctx context.Context, pid int) (string, string, error) {
	out, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "state=", "-o", "command=").Output()
	if err != nil {
		return "", "", fmt.Errorf("ps -p %d: %w", pid, err)
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return "", "", fmt.Errorf("ps -p %d: no such process", pid)
	}
	state, command, _ := strings.Cut(line, " ")
	return state, strings.TrimSpace(command), nil
}

// Kill delivers sig via the syscall; os.FindProcess cannot fail on unix.
func (ExecRunner) Kill(pid int, sig syscall.Signal) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	if err := proc.Signal(sig); err != nil {
		return fmt.Errorf("signal %d to pid %d: %w", sig, pid, err)
	}
	return nil
}

// UID returns os.Getuid().
func (ExecRunner) UID() int { return os.Getuid() }
