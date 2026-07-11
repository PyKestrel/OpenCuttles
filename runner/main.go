// Command opencuttles-runner is the on-target agent that lets an OpenCuttles
// appliance drive this desktop for agentic UI testing. It dials home (outbound
// only — no inbound ports), authenticates with a one-time enrollment token, and
// executes control commands (screenshot / click / drag / type / key) against the
// interactive desktop session. Reconnects automatically.
package main

import (
	"flag"
	"log"
	"os"
	"strings"
	"time"
)

func main() {
	var appliance, token string
	flag.StringVar(&appliance, "appliance", os.Getenv("OPENCUTTLES_APPLIANCE"), "OpenCuttles appliance base URL, e.g. http://10.1.0.104")
	flag.StringVar(&token, "token", os.Getenv("OPENCUTTLES_ENROLL_TOKEN"), "enrollment token shown in the dashboard when you add this device")
	flag.Parse()

	appliance = strings.TrimRight(strings.TrimSpace(appliance), "/")
	token = strings.TrimSpace(token)
	if appliance == "" || token == "" {
		log.Fatal("both --appliance and --token are required (or set OPENCUTTLES_APPLIANCE / OPENCUTTLES_ENROLL_TOKEN)")
	}

	screen, err := newScreen()
	if err != nil {
		log.Fatalf("desktop control unavailable: %v", err)
	}
	ctrl := &controller{screen: screen}
	log.Printf("OpenCuttles runner starting — appliance=%s", appliance)

	for {
		if err := runTunnel(appliance, token, ctrl); err != nil {
			log.Printf("tunnel closed: %v — reconnecting in 5s", err)
		}
		time.Sleep(5 * time.Second)
	}
}
