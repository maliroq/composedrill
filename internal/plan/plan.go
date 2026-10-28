package plan

import (
	"fmt"
	"strings"
	"time"

	"github.com/maliroq/composedrill/internal/model"
)

func Build(r model.ScanResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# ComposeDrill recovery plan\n# generated: %s\n# read-only: generating this plan changes nothing\n\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "Recovery readiness: %d/100\n\n", r.ReadinessScore)
	for _, p := range r.Projects {
		fmt.Fprintf(&b, "PROJECT %s\n  Compose: %s\n  SHA256: %s\n", p.Name, p.ComposeFile, p.ComposeSHA256)
		if p.GitRevision != "" {
			fmt.Fprintf(&b, "  Git revision: %s\n", p.GitRevision)
		}
		fmt.Fprintln(&b, "  Recovery order:")
		for _, s := range p.Services {
			db := ""
			if s.Database != "" {
				db = " [database: " + s.Database + "]"
			}
			fmt.Fprintf(&b, "    - %s%s\n", s.Name, db)
			if s.DatabaseStrategy != "" {
				fmt.Fprintf(&b, "      data strategy: %s\n", s.DatabaseStrategy)
			}
			for _, m := range s.Mounts {
				if m.Type == "volume" && m.Source == "" {
					fmt.Fprintf(&b, "      anonymous volume: %s (map recovery artifact by service + target)\n", m.Target)
				}
			}
			if s.Healthcheck {
				fmt.Fprintln(&b, "      verification: Docker healthcheck")
			} else {
				fmt.Fprintln(&b, "      verification: MANUAL CHECK REQUIRED unless a user-defined check is configured")
			}
		}
		if len(p.BindMounts) > 0 {
			fmt.Fprintln(&b, "  Bind mounts to map into the recovery bundle:")
			for _, m := range p.BindMounts {
				fmt.Fprintf(&b, "    - %s -> %s\n", m.Source, m.Target)
			}
		}
		if len(p.NamedVolumes) > 0 {
			fmt.Fprintln(&b, "  Named volumes to map into the recovery bundle:")
			for _, v := range p.NamedVolumes {
				fmt.Fprintf(&b, "    - %s\n", v)
			}
		}
		if len(p.Secrets) > 0 {
			fmt.Fprintln(&b, "  Secrets declared (values intentionally not shown):")
			for _, s := range p.Secrets {
				fmt.Fprintf(&b, "    - %s\n", s)
			}
		}
		fmt.Fprintln(&b, "  Isolation policy:")
		fmt.Fprintln(&b, "    - unique Compose project name")
		fmt.Fprintln(&b, "    - no production named volumes or bind paths")
		fmt.Fprintln(&b, "    - host port publications removed; HTTP/TCP probes stay inside verified recovery networks")
		fmt.Fprintln(&b, "    - recovery networks made internal to block normal routed external egress")
		fmt.Fprintln(&b, "    - cleanup limited to resources labeled for this run")
		fmt.Fprintln(&b)
	}
	fmt.Fprintln(&b, "Required proof before PASS:")
	fmt.Fprintln(&b, "  1. Compose configuration resolved without secret leakage in reports.")
	fmt.Fprintln(&b, "  2. Every restored artifact checksum matches its bundle manifest.")
	fmt.Fprintln(&b, "  3. Persistent state restores only into disposable recovery paths/volumes.")
	fmt.Fprintln(&b, "  4. Database restore uses an application-consistent strategy when declared.")
	fmt.Fprintln(&b, "  5. Containers start, then health/user checks pass. Starting alone is not PASS.")
	fmt.Fprintln(&b, "  6. Anything not safely automatable remains MANUAL CHECK REQUIRED/UNKNOWN.")
	return b.String()
}
