package gateway

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Environment variables the gateway and its launcher honour. Names are taken
// from ui-tui/src/gatewayClient.ts (resolvePython + startSpawnedGateway) and
// tui_gateway/entry.py.
const (
	// EnvPython overrides interpreter selection (checked first).
	EnvPython = "HERMES_PYTHON"
	// EnvPythonGeneric is the generic fallback override, checked second.
	EnvPythonGeneric = "PYTHON"
	// EnvVirtualEnv is consulted for <VENV>/bin/python.
	EnvVirtualEnv = "VIRTUAL_ENV"
	// EnvCWD overrides the child's working directory.
	EnvCWD = "HERMES_CWD"
	// EnvPythonPath is set to the project root so `-m tui_gateway.entry`
	// resolves against the checkout rather than the ambient environment.
	EnvPythonPath = "PYTHONPATH"
	// EnvSrcRoot is read by tui_gateway/entry.py to defeat a local package
	// shadowing the installed hermes modules. hermes_cli sets it before
	// spawning the gateway.
	EnvSrcRoot = "HERMES_PYTHON_SRC_ROOT"
	// EnvUnbuffered forces line-buffered stdout. Python's text stdout is fully
	// buffered when attached to a pipe, which is exactly this case, and a
	// buffered gateway never delivers its first frame — transport.py calls
	// this out explicitly for the HERMES_TUI_GATEWAY_NO_FLUSH knob.
	EnvUnbuffered = "PYTHONUNBUFFERED"
)

// GatewayModule is the module entry.py is reachable at.
const GatewayModule = "tui_gateway.entry"

// Launch describes how to start the Python gateway.
type Launch struct {
	// Argv is the full command, argv[0] being the interpreter.
	Argv []string
	// Env is the child environment.
	Env []string
	// Dir is the child working directory.
	Dir string
}

// CommandLine renders Argv for display. It can never contain a secret: the
// interpreter path and fixed module name are the whole command.
func (l Launch) CommandLine() string { return strings.Join(l.Argv, " ") }

// LauncherOptions configure interpreter discovery.
type LauncherOptions struct {
	// ProjectRoot is the orahermes-agent checkout holding tui_gateway/.
	ProjectRoot string
	// Python overrides discovery when non-empty (the --python flag).
	Python string
	// Environ is the environment consulted for the HERMES_PYTHON / PYTHON /
	// VIRTUAL_ENV overrides. Nil means os.Environ(). Tests inject a fixed map.
	Environ []string
}

// pythonCandidate is one step of interpreter discovery, kept as data so the
// search order can be asserted in a test rather than reimplemented there.
type pythonCandidate struct {
	path   string
	reason string
}

// DiscoverPython resolves the interpreter the way the Ink TUI does.
//
// Order (gatewayClient.ts:36-55):
//  1. $HERMES_PYTHON, then $PYTHON
//  2. $VIRTUAL_ENV/bin/python, then $VIRTUAL_ENV/Scripts/python.exe
//  3. <root>/.venv/bin/python, <root>/.venv/bin/python3,
//     <root>/venv/bin/python, <root>/venv/bin/python3
//  4. "python3" on POSIX, "python" on Windows
//
// The bare fallback is returned even though it may not exist: the caller
// reports the spawn failure with the interpreter name, which is more useful
// than a "python not found" error raised before the user asked for anything.
func DiscoverPython(opts LauncherOptions) string {
	if explicit := strings.TrimSpace(opts.Python); explicit != "" {
		return explicit
	}

	env := parseEnviron(opts.Environ)
	for _, key := range []string{EnvPython, EnvPythonGeneric} {
		if value := strings.TrimSpace(env[key]); value != "" {
			return value
		}
	}

	candidates := pythonCandidates(opts.ProjectRoot, strings.TrimSpace(env[EnvVirtualEnv]))
	for _, candidate := range candidates {
		if isExecutableFile(candidate.path) {
			return candidate.path
		}
	}

	if runtime.GOOS == "windows" {
		return "python"
	}
	return "python3"
}

// pythonCandidates lists the venv-relative interpreters in search order.
func pythonCandidates(projectRoot, virtualEnv string) []pythonCandidate {
	var candidates []pythonCandidate
	if virtualEnv != "" {
		candidates = append(candidates,
			pythonCandidate{filepath.Join(virtualEnv, "bin", "python"), "VIRTUAL_ENV/bin/python"},
			pythonCandidate{filepath.Join(virtualEnv, "Scripts", "python.exe"), "VIRTUAL_ENV/Scripts/python.exe"},
		)
	}
	if projectRoot != "" {
		candidates = append(candidates,
			pythonCandidate{filepath.Join(projectRoot, ".venv", "bin", "python"), ".venv/bin/python"},
			pythonCandidate{filepath.Join(projectRoot, ".venv", "bin", "python3"), ".venv/bin/python3"},
			pythonCandidate{filepath.Join(projectRoot, "venv", "bin", "python"), "venv/bin/python"},
			pythonCandidate{filepath.Join(projectRoot, "venv", "bin", "python3"), "venv/bin/python3"},
		)
	}
	return candidates
}

// isExecutableFile reports whether path is a regular file with any execute bit.
func isExecutableFile(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0o111 != 0
}

// parseEnviron turns a KEY=VALUE slice into a map. The last assignment wins,
// matching how a shell exports a variable twice.
func parseEnviron(environ []string) map[string]string {
	if environ == nil {
		environ = os.Environ()
	}
	out := make(map[string]string, len(environ))
	for _, entry := range environ {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		out[key] = value
	}
	return out
}

// BuildLaunch assembles the full spawn description.
//
// argv is `<python> -u -m tui_gateway.entry`. The -u is load-bearing:
// tui_gateway/transport.py documents that Python's stdout is fully buffered on
// a pipe, so without unbuffered output the gateway's frames would sit in the
// buffer and the UI would never see gateway.ready.
//
// PYTHONPATH is set to the root (prepended to any existing value) and
// HERMES_PYTHON_SRC_ROOT to the root as well, which is what entry.py reads to
// keep a local package from shadowing the installed hermes modules.
func BuildLaunch(opts LauncherOptions) (Launch, error) {
	root := strings.TrimSpace(opts.ProjectRoot)
	if root == "" {
		return Launch{}, fmt.Errorf("project root is required to locate %s", GatewayModule)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Launch{}, fmt.Errorf("resolve project root %q: %w", root, err)
	}

	env := parseEnviron(opts.Environ)
	python := DiscoverPython(opts)
	argv := []string{python, "-u", "-m", GatewayModule}

	dir := absRoot
	if override := strings.TrimSpace(env[EnvCWD]); override != "" {
		dir = override
	}

	return Launch{
		Argv: argv,
		Env:  launchEnv(env, absRoot),
		Dir:  dir,
	}, nil
}

// launchEnv builds the child environment: the inherited environment with
// PYTHONPATH prepended by root and HERMES_PYTHON_SRC_ROOT set to root.
//
// Both variables are REPLACED rather than appended. transport.py's own comment
// on the flush knob explains why position matters for correctness generally: a
// duplicate would let a stale value win depending on the reader.
func launchEnv(environ map[string]string, root string) []string {
	out := make([]string, 0, len(environ)+2)
	for key, value := range environ {
		switch key {
		case EnvPythonPath, EnvSrcRoot, EnvUnbuffered:
			continue
		default:
			out = append(out, key+"="+value)
		}
	}

	pythonPath := root
	if existing := strings.TrimSpace(environ[EnvPythonPath]); existing != "" {
		pythonPath = root + string(os.PathListSeparator) + existing
	}

	out = append(out,
		EnvPythonPath+"="+pythonPath,
		EnvSrcRoot+"="+root,
		// Belt-and-braces with argv's -u: some environments strip the flag.
		EnvUnbuffered+"=1",
	)
	return out
}

// DefaultProjectRoot walks up from the current directory looking for the
// checkout, so the binary works when built elsewhere.
//
// "Looks like the checkout" means tui_gateway/entry.py exists, which is the one
// file the gateway module cannot start without.
func DefaultProjectRoot() string {
	if wd, err := os.Getwd(); err == nil && LooksLikeProjectRoot(wd) {
		return wd
	}
	if executable, err := os.Executable(); err == nil {
		dir := filepath.Dir(executable)
		for range 6 {
			if LooksLikeProjectRoot(dir) {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// LooksLikeProjectRoot reports whether dir is an orahermes-agent checkout.
func LooksLikeProjectRoot(dir string) bool {
	if dir == "" {
		return false
	}
	entry := filepath.Join(dir, "tui_gateway", "entry.py")
	info, err := os.Stat(entry)
	return err == nil && !info.IsDir()
}
