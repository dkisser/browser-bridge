package cli

import (
	"errors"
	"strings"
	"testing"
)

// TestServiceHelp: bare `bridge service` prints the bash service help.
func TestServiceHelp(t *testing.T) {
	stdout, _, err := runCLI(t, "service")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout, "Usage: bridge service <command> [args]") {
		t.Errorf("stdout = %q", stdout)
	}
	for _, verb := range []string{"up [--foreground]", "down", "restart", "status", "logs", "enable", "disable", "update [version]", "doctor", "version", "uninstall"} {
		if !strings.Contains(stdout, verb) {
			t.Errorf("help missing %q:\n%s", verb, stdout)
		}
	}
}

// TestServiceUnknownSubcommand: an unknown verb under service is BB-E306,
// not cobra's suggestion text.
func TestServiceUnknownSubcommand(t *testing.T) {
	_, stderr, err := runCLI(t, "service", "bogus")
	if !errors.Is(err, ErrReported) {
		t.Fatalf("err = %v, want ErrReported", err)
	}
	want := "Error: BB-E306: unknown service command 'bogus'. Run 'bridge service' for the list.\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// TestMovedVerbs: every pre-service-namespace top-level lifecycle verb dies
// with BB-E305 pointing at the new location.
func TestMovedVerbs(t *testing.T) {
	for _, verb := range []string{"up", "down", "restart", "status", "logs", "update", "doctor", "uninstall", "version"} {
		t.Run(verb, func(t *testing.T) {
			_, stderr, err := runCLI(t, verb)
			if !errors.Is(err, ErrReported) {
				t.Fatalf("err = %v, want ErrReported", err)
			}
			want := "Error: BB-E305: '" + verb + "' has moved: lifecycle commands live under 'bridge service' — try 'bridge service " + verb + "'.\n"
			if stderr != want {
				t.Errorf("stderr = %q, want %q", stderr, want)
			}
		})
	}
}

// TestServiceJSONError: --json maps a service failure to the envelope with
// the BB-E### code as the error kind.
func TestServiceJSONError(t *testing.T) {
	t.Setenv("BB_HOME", t.TempDir())
	t.Setenv("BB_EXTENSION_DIR", t.TempDir())
	stdout, stderr, err := runCLI(t, "--json", "service", "up")
	if !errors.Is(err, ErrReported) {
		t.Fatalf("err = %v, want ErrReported", err)
	}
	want := "{\"status\":\"error\",\"error\":\"BB-E002\",\"message\":\"BB-E002: install not run. Execute the install script first.\"}\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

// TestServiceStatusExitCode: a stopped service is exit 1 (ErrSilent) with
// the status lines on stdout.
func TestServiceStatusExitCode(t *testing.T) {
	t.Setenv("BB_HOME", t.TempDir())
	t.Setenv("BB_EXTENSION_DIR", t.TempDir())
	stdout, _, err := runCLI(t, "service", "status")
	if !errors.Is(err, ErrSilent) {
		t.Fatalf("err = %v, want ErrSilent", err)
	}
	if !strings.Contains(stdout, "bridge-core:  stopped") {
		t.Errorf("stdout = %q", stdout)
	}
}

// TestVersionFlag: --version prints "bridge <version>" like the bash router.
func TestVersionFlag(t *testing.T) {
	stdout, _, err := runCLI(t, "--version")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if stdout != "bridge test\n" {
		t.Errorf("stdout = %q, want %q", stdout, "bridge test\n")
	}
}
