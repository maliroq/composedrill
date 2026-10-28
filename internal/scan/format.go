package scan

import (
	"fmt"
	"io"

	"github.com/maliroq/composedrill/internal/model"
)

func Print(w io.Writer, r model.ScanResult) {
	fmt.Fprintf(w, "Detected:\n  %d Compose projects\n  %d services\n  %d named volumes\n  %d bind mounts\n  %d databases\n  %d services with health checks\n  %d unpinned images\n\nRecovery readiness: %d/100\n", len(r.Projects), r.ServiceCount, r.NamedVolumeCount, r.BindMountCount, r.DatabaseCount, r.HealthcheckCount, r.UnpinnedImages, r.ReadinessScore)
	if len(r.Findings) == 0 {
		fmt.Fprintln(w, "\nNo static findings.")
		return
	}
	fmt.Fprintln(w, "\nFindings:")
	for _, f := range r.Findings {
		scope := f.Project
		if f.Service != "" {
			scope += "/" + f.Service
		}
		fmt.Fprintf(w, "  %-5s %-24s %s\n", f.Severity, scope, f.Message)
	}
	fmt.Fprintln(w, "\nStatic readiness is not recovery proof. Run `composedrill rehearse` with a mapped recovery bundle to prove restoration.")
}
