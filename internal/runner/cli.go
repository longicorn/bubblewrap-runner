package runner

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("path must not be empty")
	}
	*s = append(*s, value)
	return nil
}

type options struct {
	ro, rw, deny stringList
	configPath   string
	dryRun       bool
	noNet        bool
}

func Main(args []string) (int, error) {
	if len(args) > 0 && args[0] == "init" {
		if len(args) != 1 {
			return 2, errors.New("usage: bwrun init")
		}
		if err := initializeProject(); err != nil {
			return 1, err
		}
		return 0, nil
	}

	fs := flag.NewFlagSet("bwrun", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var opts options
	fs.Var(&opts.ro, "ro", "expose a host path read-only (repeatable)")
	fs.Var(&opts.rw, "rw", "expose a host path read-write (repeatable)")
	fs.Var(&opts.deny, "deny", "hide a host path (repeatable)")
	fs.StringVar(&opts.configPath, "config", "", "use only this configuration file (instead of discovering global and project files)")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "print the generated bubblewrap command")
	fs.BoolVar(&opts.noNet, "no-net", false, "disable network access")
	if err := fs.Parse(args); err != nil {
		return 2, err
	}
	command := fs.Args()
	if len(command) > 0 && command[0] == "--" {
		command = command[1:]
	}
	if len(command) == 0 {
		return 2, errors.New("usage: bwrun init | bwrun [flags] [--] <command> [args...]")
	}

	plan, err := buildPlan(opts, command)
	if err != nil {
		return 1, err
	}
	if opts.dryRun {
		bwrap, err := exec.LookPath("bwrap")
		if err != nil {
			bwrap = "bwrap"
		}
		fmt.Fprintln(os.Stdout, shellJoin(append([]string{bwrap}, plan.args...)))
		fmt.Fprintln(os.Stdout, "# sandbox environment (values omitted):", strings.Join(envNames(plan.env), ", "))
		return 0, nil
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return 127, errors.New("bwrap executable was not found in PATH")
	}
	return runChild(bwrap, plan)
}
