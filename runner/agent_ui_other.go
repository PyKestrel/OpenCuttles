//go:build !windows

package main

// runAgentUI on non-Windows platforms just runs the reconnecting tunnel loop;
// there is no tray UI yet (the Linux/macOS agents run headless from their
// autostart entries).
func runAgentUI(e enrollment, st *agentState) {
	runAgentLoop(e.Appliance, e.Token, st)
}

// maybeShowWizard is a no-op off Windows; those platforms fall back to the CLI
// usage message when launched without credentials.
func maybeShowWizard() bool { return false }
