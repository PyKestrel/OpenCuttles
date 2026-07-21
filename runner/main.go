// Command opencuttles-runner is the on-target agent that lets an OpenCuttles
// appliance drive this desktop for agentic UI testing. It dials home (outbound
// only — no inbound ports), authenticates with a one-time enrollment token, and
// executes control commands (screenshot / click / drag / type / key) against the
// interactive desktop session. Reconnects automatically.
//
// Usage:
//
//	opencuttles-runner --appliance URL --token TOKEN [--pin sha256/...]
//	                                                     run in the foreground
//	opencuttles-runner install --appliance URL --token TOKEN [--pin sha256/...]
//	                                                     install to auto-start at
//	                                                     login, then start now
//	opencuttles-runner uninstall                         remove the auto-start entry
//
// The appliance URL must be https. A bare host is assumed to be https; plaintext
// http is refused unless --insecure is passed, because this channel carries
// device control and build artifacts the runner downloads and executes.
//
// --pin authenticates a self-signed appliance certificate by its public key,
// which is what makes TLS usable for an appliance reached by IP address.
package main

import (
	"crypto/tls"
	"errors"
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
	appliance := fs.String("appliance", os.Getenv("OPENCUTTLES_APPLIANCE"), "OpenCuttles appliance base URL, e.g. https://testral.example (a bare host is assumed to be https)")
	token := fs.String("token", os.Getenv("OPENCUTTLES_ENROLL_TOKEN"), "enrollment token shown in the dashboard when you add this device")
	pin := fs.String("pin", os.Getenv("OPENCUTTLES_APPLIANCE_PIN"), "appliance certificate pin (sha256/BASE64), shown with the enrollment token; required for a self-signed appliance")
	insecure := fs.Bool("insecure", false, "allow plaintext HTTP and skip certificate verification — development only, never for a real device")
	identitySrc := fs.String("identity", os.Getenv("OPENCUTTLES_IDENTITY_FILE"), "path to the client identity bundle from enrollment; only needed when the appliance requires mutual TLS")
	_ = fs.Parse(rest)

	tok := strings.TrimSpace(*token)
	e := enrollment{Token: tok, Pin: strings.TrimSpace(*pin), Insecure: *insecure, IdentitySrc: strings.TrimSpace(*identitySrc)}

	// Resolve the appliance URL up front so a bad scheme fails immediately with a
	// clear message, rather than at the first request.
	rawAppliance := strings.TrimSpace(*appliance)
	if rawAppliance != "" {
		normalized, err := normalizeAppliance(rawAppliance, *insecure)
		if err != nil {
			log.Fatalf("%v", err)
		}
		e.Appliance = normalized
	}

	pinBytes, err := parsePin(e.Pin)
	if err != nil {
		log.Fatalf("invalid --pin: %v", err)
	}
	if *insecure {
		log.Printf("WARNING: --insecure is set — the connection to %s is unverified "+
			"and may be plaintext. Never use this for a device that matters.", e.Appliance)
	} else if len(pinBytes) == 0 {
		log.Printf("no --pin given; verifying the appliance against the system trust store")
	}

	// The client identity is presented whenever one is present. Appliances that
	// don't require mutual TLS ignore it, so there is no mode to get wrong.
	clientCert := loadClientIdentity(e.IdentitySrc)
	configureTLS(pinBytes, *insecure, clientCert)

	switch sub {
	case "uninstall":
		if err := runUninstall(); err != nil {
			log.Fatalf("uninstall failed: %v", err)
		}
		fmt.Println("OpenCuttles runner auto-start removed.")
	case "install":
		requireCreds(e.Appliance, e.Token)
		if err := runInstall(e); err != nil {
			log.Fatalf("install failed: %v", err)
		}
	case "":
		if e.Appliance == "" || e.Token == "" {
			// Launched with no credentials (e.g. double-clicked from Explorer):
			// on Windows offer the install wizard; otherwise print the usage.
			if maybeShowWizard() {
				return
			}
			requireCreds(e.Appliance, e.Token)
		}
		runAgent(e)
	default:
		log.Fatalf("unknown command %q (expected: install, uninstall, or no command to run)", sub)
	}
}

// loadClientIdentity resolves the client certificate for mutual TLS, preferring
// an explicit --identity path over the installed one.
//
// Absence is normal and silent — mutual TLS is opt-in and most appliances don't
// use it. A file that exists but cannot be used is fatal: continuing would mean
// dialing an appliance that requires a certificate without one, and failing at
// the handshake with a TLS error that says nothing about the real cause.
func loadClientIdentity(explicit string) *tls.Certificate {
	path := explicit
	if path == "" {
		path = identityPath()
	}
	cert, err := loadIdentity(path)
	switch {
	case errors.Is(err, errNoIdentity):
		if explicit != "" {
			// Explicitly asked for, so silence would be wrong.
			log.Fatalf("no client identity at %s", explicit)
		}
		return nil
	case err != nil:
		log.Fatalf("%v", err)
	}
	if cn, notAfter := certExpiry(cert); cn != "" {
		log.Printf("presenting the client certificate for device %s (valid until %s)", cn, notAfter)
	}
	return cert
}

func requireCreds(appliance, token string) {
	if appliance == "" || token == "" {
		log.Fatal("both --appliance and --token are required (or set OPENCUTTLES_APPLIANCE / OPENCUTTLES_ENROLL_TOKEN)")
	}
}

// runAgent runs the tunnel. On Windows it also shows the system-tray UI; on
// other platforms it just runs the reconnecting loop (runAgentUI is defined
// per-platform).
func runAgent(e enrollment) {
	logPath := setupFileLog()
	st := newAgentState(e.Appliance, logPath)
	runAgentUI(e, st)
}
