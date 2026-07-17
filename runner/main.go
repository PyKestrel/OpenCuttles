// Command opencuttles-runner is the on-target agent that lets an OpenCuttles
// appliance drive this desktop for agentic UI testing. It dials home (outbound
// only — no inbound ports), authenticates with a one-time enrollment token, and
// executes control commands (screenshot / click / drag / type / key) against the
// interactive desktop session. Reconnects automatically.
//
// Usage:
//
//	opencuttles-runner --appliance URL --token TOKEN     run in the foreground
//	opencuttles-runner install --appliance URL --token TOKEN
//	                                                     install to auto-start at
//	                                                     login, then start now
//	opencuttles-runner uninstall                         remove the auto-start entry
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
)

func main() {
	// Optional leading subcommand (install/uninstall); everything else is a flag,
	// so a bare `--appliance … --token …` still runs the agent as before.
	sub := ""
	rest := os.Args[1:]
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		sub, rest = rest[0], rest[1:]
	}

	fs := flag.NewFlagSet("opencuttles-runner", flag.ExitOnError)
	appliance := fs.String("appliance", os.Getenv("OPENCUTTLES_APPLIANCE"), "OpenCuttles appliance base URL, e.g. http://10.1.0.104")
	token := fs.String("token", os.Getenv("OPENCUTTLES_ENROLL_TOKEN"), "enrollment token shown in the dashboard when you add this device")
	_ = fs.Parse(rest)

	appl := strings.TrimRight(strings.TrimSpace(*appliance), "/")
	tok := strings.TrimSpace(*token)

	switch sub {
	case "uninstall":
		if err := runUninstall(); err != nil {
			log.Fatalf("uninstall failed: %v", err)
		}
		fmt.Println("OpenCuttles runner auto-start removed.")
	case "install":
		requireCreds(appl, tok)
		if err := runInstall(appl, tok); err != nil {
			log.Fatalf("install failed: %v", err)
		}
	case "":
		if appl == "" || tok == "" {
			// Launched with no credentials (e.g. double-clicked from Explorer):
			// on Windows offer the install wizard; otherwise print the usage.
			if maybeShowWizard() {
				return
			}
			requireCreds(appl, tok)
		}
		runAgent(appl, tok)
	default:
		log.Fatalf("unknown command %q (expected: install, uninstall, or no command to run)", sub)
	}
}

func requireCreds(appliance, token string) {
	if appliance == "" || token == "" {
		log.Fatal("both --appliance and --token are required (or set OPENCUTTLES_APPLIANCE / OPENCUTTLES_ENROLL_TOKEN)")
	}
}

// runAgent runs the tunnel. On Windows it also shows the system-tray UI; on
// other platforms it just runs the reconnecting loop (runAgentUI is defined
// per-platform).
func runAgent(appliance, token string) {
	logPath := setupFileLog()
	st := newAgentState(appliance, logPath)
	runAgentUI(appliance, token, st)
}
