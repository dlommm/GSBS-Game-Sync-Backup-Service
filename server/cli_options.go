package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

type cliOptions struct {
	showVersion      bool
	serviceMode      bool
	installService   bool
	uninstallService bool
	startService     bool
	stopService      bool
	envFile          string
}

func parseCLIOptions(args []string) (cliOptions, error) {
	var opts cliOptions
	fs := flag.NewFlagSet("gsbs-server", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var versionAlt bool
	fs.BoolVar(&opts.showVersion, "version", false, "print version and exit")
	fs.BoolVar(&versionAlt, "v", false, "print version and exit")
	fs.BoolVar(&opts.serviceMode, "service", false, "run as Windows service host")
	fs.BoolVar(&opts.installService, "install-service", false, "install Windows service")
	fs.BoolVar(&opts.uninstallService, "uninstall-service", false, "uninstall Windows service")
	fs.BoolVar(&opts.startService, "start-service", false, "start Windows service")
	fs.BoolVar(&opts.stopService, "stop-service", false, "stop Windows service")
	fs.StringVar(&opts.envFile, "env-file", "", "path to KEY=VALUE env file loaded before startup")

	if err := fs.Parse(args); err != nil {
		return opts, fmt.Errorf("%w\n\n%s", err, cliUsage())
	}
	if versionAlt {
		opts.showVersion = true
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("unknown arguments: %s\n\n%s", strings.Join(fs.Args(), " "), cliUsage())
	}

	modes := 0
	if opts.serviceMode {
		modes++
	}
	if opts.installService {
		modes++
	}
	if opts.uninstallService {
		modes++
	}
	if opts.startService {
		modes++
	}
	if opts.stopService {
		modes++
	}
	if modes > 1 {
		return opts, fmt.Errorf("service flags are mutually exclusive\n\n%s", cliUsage())
	}
	return opts, nil
}

func cliUsage() string {
	return strings.TrimSpace(`
Usage:
  gsbs-server [flags]

General:
  --version, -v           Print version and exit

Windows service:
  --service               Run in service host mode (for SCM)
  --install-service       Install service (GSBSServer)
  --uninstall-service     Uninstall service (GSBSServer)
  --start-service         Start installed service
  --stop-service          Stop installed service

Runtime:
  --env-file PATH         Load env vars from PATH before startup
`)
}
