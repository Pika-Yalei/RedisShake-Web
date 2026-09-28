// Run the centralized tests in their original Go packages using a build overlay.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		fmt.Println("Usage: go run ./tests [test|vet|list] [Go flags and package paths]\nDefault: test ./...\nExample: go run ./tests test -race ./...")
		return nil
	}
	command := "test"
	if len(args) > 0 && (args[0] == "test" || args[0] == "vet" || args[0] == "list") {
		command, args = args[0], args[1:]
	}
	if len(args) == 0 {
		args = []string{"./..."}
	}
	for _, arg := range args {
		if arg == "-overlay" || strings.HasPrefix(arg, "-overlay=") {
			return errors.New("the test runner manages -overlay; do not pass another overlay")
		}
	}
	repo, err := os.Getwd()
	if err != nil {
		return err
	}
	for _, path := range []string{"go.mod", "tests/run.go"} {
		if _, err := os.Stat(filepath.Join(repo, path)); err != nil {
			return errors.New("run this command from the repository root")
		}
	}

	// The package-relative paths preserve access to unexported functions and
	// keep //go:embed paths and test working directories unchanged.
	testDir := filepath.Join(repo, "tests")
	replace := make(map[string]string)
	err = filepath.WalkDir(testDir, func(source string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(testDir, source)
		if err != nil {
			return err
		}
		target := filepath.Join(repo, rel)
		if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("test overlay target must not exist: %s", target)
		}
		if info, err := os.Stat(filepath.Dir(target)); err != nil || !info.IsDir() {
			return fmt.Errorf("test has no matching source package: %s", source)
		}
		replace[target] = source
		// Hide the backing copy from package discovery to avoid compiling it
		// as a separate package without the production source files.
		replace[source] = ""
		return nil
	})
	if err != nil {
		return err
	}
	if len(replace) == 0 {
		return errors.New("no centralized test files found")
	}
	data, err := json.Marshal(struct {
		Replace map[string]string
	}{replace})
	if err != nil {
		return err
	}
	overlay, err := os.CreateTemp("", "redis-shake-web-tests-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(overlay.Name())
	if _, err := overlay.Write(data); err != nil {
		overlay.Close()
		return err
	}
	if err := overlay.Close(); err != nil {
		return err
	}
	goArgs := append([]string{command, "-overlay=" + overlay.Name()}, args...)
	cmd := exec.Command("go", goArgs...)
	cmd.Dir = repo
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
