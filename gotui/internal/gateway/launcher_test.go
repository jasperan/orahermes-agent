package gateway

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeExecutable creates a file that passes isExecutableFile, so venv discovery
// can be exercised without a real interpreter.
func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// fakeCheckout builds a directory that LooksLikeProjectRoot accepts.
func fakeCheckout(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tui_gateway"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "tui_gateway", "entry.py"), []byte("# entry\n"), 0o644); err != nil {
		t.Fatalf("write entry.py: %v", err)
	}
	return root
}

// TestDiscoverPythonPrecedence pins the search order gatewayClient.ts:36-55
// documents. Each case isolates exactly one step so a reordering fails loudly.
func TestDiscoverPythonPrecedence(t *testing.T) {
	root := fakeCheckout(t)
	writeExecutable(t, filepath.Join(root, ".venv", "bin", "python"))
	writeExecutable(t, filepath.Join(root, ".venv", "bin", "python3"))
	writeExecutable(t, filepath.Join(root, "venv", "bin", "python"))

	venvDir := t.TempDir()
	venvPython := filepath.Join(venvDir, "bin", "python")
	writeExecutable(t, venvPython)

	t.Run("explicit flag wins over everything", func(t *testing.T) {
		got := DiscoverPython(LauncherOptions{
			ProjectRoot: root,
			Python:      "/opt/custom/python",
			Environ:     []string{EnvPython + "=/from/env"},
		})
		if got != "/opt/custom/python" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("HERMES_PYTHON beats PYTHON and the venv", func(t *testing.T) {
		got := DiscoverPython(LauncherOptions{
			ProjectRoot: root,
			Environ:     []string{EnvPython + "=/from/hermes", EnvPythonGeneric + "=/from/python"},
		})
		if got != "/from/hermes" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("PYTHON is the second override", func(t *testing.T) {
		got := DiscoverPython(LauncherOptions{ProjectRoot: root, Environ: []string{EnvPythonGeneric + "=/from/python"}})
		if got != "/from/python" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("VIRTUAL_ENV beats the checkout's venv", func(t *testing.T) {
		got := DiscoverPython(LauncherOptions{
			ProjectRoot: root,
			Environ:     []string{EnvVirtualEnv + "=" + venvDir},
		})
		if got != venvPython {
			t.Fatalf("got %q, want %q", got, venvPython)
		}
	})

	t.Run("checkout .venv when nothing is exported", func(t *testing.T) {
		got := DiscoverPython(LauncherOptions{ProjectRoot: root, Environ: []string{}})
		want := filepath.Join(root, ".venv", "bin", "python")
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("falls back to a bare interpreter", func(t *testing.T) {
		got := DiscoverPython(LauncherOptions{ProjectRoot: t.TempDir(), Environ: []string{}})
		want := "python3"
		if runtime.GOOS == "windows" {
			want = "python"
		}
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

// TestDiscoverPythonSkipsNonExecutableFiles is the guard against binding to a
// broken symlink or a package directory named python.
func TestDiscoverPythonSkipsNonExecutableFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the execute bit is POSIX-only")
	}
	root := fakeCheckout(t)
	// A non-executable file where the interpreter is expected.
	path := filepath.Join(root, ".venv", "bin", "python")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := DiscoverPython(LauncherOptions{ProjectRoot: root, Environ: []string{}})
	if got == path {
		t.Fatal("a non-executable file must not be chosen as the interpreter")
	}
}

func TestBuildLaunchArgv(t *testing.T) {
	root := fakeCheckout(t)
	launch, err := BuildLaunch(LauncherOptions{ProjectRoot: root, Python: "/usr/bin/python3", Environ: []string{}})
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}

	want := []string{"/usr/bin/python3", "-u", "-m", GatewayModule}
	if len(launch.Argv) != len(want) {
		t.Fatalf("argv = %v", launch.Argv)
	}
	for i := range want {
		if launch.Argv[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q", i, launch.Argv[i], want[i])
		}
	}
	// -u is load-bearing: without it the gateway's frames sit in Python's
	// pipe buffer and gateway.ready never arrives.
	if !strings.Contains(launch.CommandLine(), " -u ") {
		t.Fatal("argv must request unbuffered stdout")
	}
}

func TestBuildLaunchEnvironment(t *testing.T) {
	root := fakeCheckout(t)
	launch, err := BuildLaunch(LauncherOptions{
		ProjectRoot: root,
		Python:      "python3",
		Environ:     []string{EnvPythonPath + "=/pre/existing", "KEEP=me"},
	})
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}

	env := parseEnviron(launch.Env)
	if env["KEEP"] != "me" {
		t.Fatal("the inherited environment must be preserved")
	}
	wantPath := root + string(os.PathListSeparator) + "/pre/existing"
	if env[EnvPythonPath] != wantPath {
		t.Fatalf("PYTHONPATH = %q, want %q", env[EnvPythonPath], wantPath)
	}
	if env[EnvSrcRoot] != root {
		t.Fatalf("PYTHONPATH_SRC_ROOT = %q, want %q", env[EnvSrcRoot], root)
	}
	if env[EnvUnbuffered] != "1" {
		t.Fatal("PYTHONUNBUFFERED must be set as a second guarantee of unbuffered output")
	}

	// Exactly one assignment per managed key: a duplicate would let a stale
	// value win depending on the reader.
	for _, key := range []string{EnvPythonPath, EnvSrcRoot, EnvUnbuffered} {
		count := 0
		for _, entry := range launch.Env {
			if strings.HasPrefix(entry, key+"=") {
				count++
			}
		}
		if count != 1 {
			t.Errorf("%s appears %d times, want exactly 1", key, count)
		}
	}
}

func TestBuildLaunchUsesHermesCwdOverride(t *testing.T) {
	root := fakeCheckout(t)
	override := t.TempDir()
	launch, err := BuildLaunch(LauncherOptions{
		ProjectRoot: root,
		Python:      "python3",
		Environ:     []string{EnvCWD + "=" + override},
	})
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if launch.Dir != override {
		t.Fatalf("Dir = %q, want %q", launch.Dir, override)
	}
}

func TestBuildLaunchResolvesRelativeRoot(t *testing.T) {
	root := fakeCheckout(t)
	// chdir so a relative path is meaningful, and restore afterwards.
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	if err := os.Chdir(filepath.Dir(root)); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	launch, err := BuildLaunch(LauncherOptions{
		ProjectRoot: filepath.Base(root),
		Python:      "python3",
		Environ:     []string{},
	})
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if !filepath.IsAbs(launch.Dir) {
		t.Fatalf("Dir = %q, want an absolute path", launch.Dir)
	}
}

func TestBuildLaunchRequiresRoot(t *testing.T) {
	if _, err := BuildLaunch(LauncherOptions{Python: "python3"}); err == nil {
		t.Fatal("a missing project root must be an error, not a silent fallback")
	}
}

func TestLooksLikeProjectRoot(t *testing.T) {
	if !LooksLikeProjectRoot(fakeCheckout(t)) {
		t.Fatal("a directory with tui_gateway/entry.py is a checkout")
	}
	if LooksLikeProjectRoot(t.TempDir()) {
		t.Fatal("an empty directory is not a checkout")
	}
	if LooksLikeProjectRoot("") {
		t.Fatal("an empty path is not a checkout")
	}

	// A directory named tui_gateway but with no entry.py is not a checkout.
	partial := t.TempDir()
	if err := os.MkdirAll(filepath.Join(partial, "tui_gateway"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if LooksLikeProjectRoot(partial) {
		t.Fatal("missing entry.py must not count as a checkout")
	}
}

// TestLooksLikeProjectRootAcceptsRealCheckout runs against the actual repository
// when the test is executed inside it, which is the case that matters.
func TestLooksLikeProjectRootAcceptsRealCheckout(t *testing.T) {
	if !LooksLikeProjectRoot("../../..") {
		t.Skip("not running inside an orahermes-agent checkout")
	}
}
