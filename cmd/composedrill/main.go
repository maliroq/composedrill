package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/maliroq/composedrill/internal/manifest"
	"github.com/maliroq/composedrill/internal/plan"
	"github.com/maliroq/composedrill/internal/rehearse"
	"github.com/maliroq/composedrill/internal/scan"
)

const version = "0.1.0-preview"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "scan":
		err = cmdScan(os.Args[2:])
	case "plan":
		err = cmdPlan(os.Args[2:])
	case "manifest":
		err = cmdManifest(os.Args[2:])
	case "rehearse":
		err = cmdRehearse(os.Args[2:])
	case "cleanup":
		err = cmdCleanup(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("composedrill", version)
		return
	case "help", "--help", "-h":
		usage()
		return
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
func usage() {
	fmt.Print(`ComposeDrill — prove Docker Compose recovery before disaster.

Usage:
  composedrill scan [path] [--json]
  composedrill plan [path] [--output FILE]
  composedrill manifest [path] [--output FILE]
  composedrill rehearse [path] --backup PATH [--checks FILE] [--dry-run] [--keep]
  composedrill cleanup --run RUN_ID [--root PATH]
  composedrill version

scan, plan, and manifest are read-only. Rehearse only creates namespaced resources labeled
io.composedrill.managed=true and refuses unsafe host/privileged runtime modes.
`)
}
func cmdScan(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	js := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	r, err := scan.Run(root)
	if err != nil {
		return err
	}
	if *js {
		b, _ := json.MarshalIndent(r, "", "  ")
		fmt.Println(string(b))
	} else {
		scan.Print(os.Stdout, r)
	}
	return nil
}
func cmdPlan(args []string) error {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	out := fs.String("output", "", "write plan to file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	r, err := scan.Run(root)
	if err != nil {
		return err
	}
	txt := plan.Build(r)
	if *out == "" {
		fmt.Print(txt)
		return nil
	}
	return os.WriteFile(*out, []byte(txt), 0600)
}
func cmdManifest(args []string) error {
	fs := flag.NewFlagSet("manifest", flag.ContinueOnError)
	out := fs.String("output", "recovery-manifest.json", "output file, or - for stdout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	r, err := scan.Run(root)
	if err != nil {
		return err
	}
	m := manifest.Build(r, version)
	b, _ := json.MarshalIndent(m, "", "  ")
	if *out == "-" {
		fmt.Println(string(b))
		return nil
	}
	return os.WriteFile(*out, b, 0600)
}
func cmdRehearse(args []string) error {
	fs := flag.NewFlagSet("rehearse", flag.ContinueOnError)
	backup := fs.String("backup", "", "recovery bundle directory or tar archive")
	checks := fs.String("checks", "", "declared/checksummed checks JSON inside the recovery bundle; defaults to bundle/composedrill-checks.json")
	dry := fs.Bool("dry-run", false, "resolve, validate, and plan without creating Docker resources")
	keep := fs.Bool("keep", false, "keep isolated recovery resources for inspection")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	if *backup == "" {
		return fmt.Errorf("--backup is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rep, err := rehearse.Run(ctx, rehearse.Options{Root: root, Backup: *backup, ChecksFile: *checks, DryRun: *dry, Keep: *keep, Version: version})
	if err != nil {
		return err
	}
	printReport(rep)
	if rep.Status == "FAIL" {
		return fmt.Errorf("recovery rehearsal failed; evidence: %s", rep.EvidencePath)
	}
	return nil
}
func printReport(r any) {
	b, _ := json.MarshalIndent(r, "", "  ")
	var rep map[string]any
	_ = json.Unmarshal(b, &rep)
	svcs, _ := rep["services"].([]any)
	color := colorEnabled(os.Stdout)

	fmt.Printf("ComposeDrill recovery drill\nrun: %v\n\n", rep["run_id"])
	fmt.Println("SUMMARY")
	printStatusLine("BACKUP / MANIFEST", backupManifestStatus(rep), color)
	printStatusLine("DATA RESTORE", aggregateServiceStatus(svcs, "data_restore"), color)
	printStatusLine("CONTAINER STARTUP", aggregateServiceStatus(svcs, "startup"), color)
	printStatusLine("APPLICATION HEALTH", aggregateServiceStatus(svcs, "health"), color)
	fmt.Println()
	printStatusLine("RESULT", fmt.Sprint(rep["status"]), color)

	for _, x := range svcs {
		m, _ := x.(map[string]any)
		fmt.Printf("\n%s\n", strings.ToUpper(fmt.Sprint(m["service"])))
		printStatusLine("configuration", fmt.Sprint(m["configuration"]), color)
		printStatusLine("data restore", fmt.Sprint(m["data_restore"]), color)
		printStatusLine("startup", fmt.Sprint(m["startup"]), color)
		printStatusLine("health", fmt.Sprint(m["health"]), color)
		if reason := fmt.Sprint(m["reason"]); reason != "" && reason != "<nil>" {
			fmt.Printf("  %-22s %s\n", "reason", reason)
		}
	}

	fmt.Println()
	printStatusLine("CLEANUP", fmt.Sprint(rep["cleanup_status"]), color)
	fmt.Printf("%-24s %v\n", "EVIDENCE", rep["evidence_path"])
}

func backupManifestStatus(rep map[string]any) string {
	v, ok := rep["backup_manifest_sha256"]
	if !ok || v == nil || strings.TrimSpace(fmt.Sprint(v)) == "" {
		return "UNKNOWN"
	}
	return "PASS"
}

func aggregateServiceStatus(svcs []any, key string) string {
	if len(svcs) == 0 {
		return "UNKNOWN"
	}
	seen := map[string]bool{}
	for _, x := range svcs {
		m, _ := x.(map[string]any)
		seen[strings.ToUpper(strings.TrimSpace(fmt.Sprint(m[key])))] = true
	}
	for _, status := range []string{"FAIL", "MANUAL CHECK REQUIRED", "UNKNOWN", "NOT RUN", "DRY RUN", "PASS"} {
		if seen[status] {
			return status
		}
	}
	return "UNKNOWN"
}

func printStatusLine(label, status string, color bool) {
	fmt.Printf("%-24s %s\n", label, colorStatus(status, color))
}

func colorEnabled(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func colorStatus(status string, enabled bool) string {
	if !enabled {
		return status
	}
	code := ""
	switch strings.ToUpper(status) {
	case "PASS":
		code = "\x1b[32m"
	case "FAIL":
		code = "\x1b[31m"
	case "MANUAL CHECK REQUIRED", "UNKNOWN", "NOT RUN", "DRY RUN":
		code = "\x1b[33m"
	default:
		return status
	}
	return code + status + "\x1b[0m"
}
func cmdCleanup(args []string) error {
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	run := fs.String("run", "", "ComposeDrill run ID")
	root := fs.String("root", ".", "root containing .composedrill run workspace")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *run == "" {
		return fmt.Errorf("--run is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	project := rehearse.ProjectNameForRun(*run)
	if err := rehearse.CleanupRun(ctx, *run, project); err != nil {
		return err
	}
	if err := rehearse.CleanupWorkspace(*root, *run); err != nil {
		return err
	}
	fmt.Printf("cleaned ComposeDrill resources and run workspace for %s\n", *run)
	return nil
}
