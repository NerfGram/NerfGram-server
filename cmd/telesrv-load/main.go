// Command telesrv-load provisions and drives real encrypted MTProto sessions.
// It is intentionally separate from the server process so a load generator can
// run on the M2 host without sharing server memory, database connections or
// internal handler shortcuts.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"telesrv/internal/loadharness"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "telesrv-load:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "keygen":
		return runKeygen(args[1:])
	case "provision":
		return runProvision(ctx, args[1:])
	case "run":
		return runLoad(ctx, args[1:])
	case "summarize":
		return runSummarize(args[1:])
	case "help", "-h", "--help":
		fmt.Fprintln(os.Stdout, usageText)
		return nil
	default:
		return usageError()
	}
}

func runKeygen(args []string) error {
	flags := flag.NewFlagSet("keygen", flag.ContinueOnError)
	path := flags.String("out", filepath.FromSlash("data/loadtest/session.key"), "owner-only session encryption key file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("keygen accepts no positional arguments")
	}
	if err := loadharness.GenerateSessionKey(*path); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "session encryption key written to %s\n", *path)
	return nil
}

func runProvision(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("provision", flag.ContinueOnError)
	manifest := flags.String("manifest", filepath.FromSlash("data/loadtest/manifest.json"), "output manifest")
	sessionKey := flags.String("session-key", filepath.FromSlash("data/loadtest/session.key"), "session encryption key")
	server := flags.String("server", "127.0.0.1:2398", "MTProto server address")
	dc := flags.Int("dc", 2, "wire DC label")
	rsaKey := flags.String("rsa-key", filepath.FromSlash("data/server_rsa.pem"), "server RSA private/public PEM")
	apiID := flags.Int("api-id", 1, "test application ID")
	apiHash := flags.String("api-hash", "hash", "test application hash")
	accounts := flags.Int("accounts", 450, "unique accounts")
	extraDevices := flags.Int("extra-devices", 50, "accounts receiving a second independent session")
	concurrency := flags.Int("concurrency", 8, "parallel provisioning workers (max 64)")
	phonePrefix := flags.String("phone-prefix", "+155500", "E.164 prefix followed by a six-digit account index")
	firstName := flags.String("first-name-prefix", "Load", "generated first-name prefix")
	obfuscated := flags.Bool("obfuscated", true, "use TDesktop-like Obfuscated2 + abridged transport")
	pfs := flags.Bool("pfs", true, "bind temporary auth keys using PFS")
	tempKeyTTL := flags.Int("temp-key-ttl", 86400, "temporary auth-key lifetime in seconds")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("provision accepts no positional arguments")
	}
	code := os.Getenv("TELESRV_LOAD_LOGIN_CODE")
	if code == "" {
		return errors.New("TELESRV_LOAD_LOGIN_CODE must contain the test environment login code")
	}
	cfg := loadharness.ProvisionConfig{
		ManifestPath: *manifest, SessionKeyPath: *sessionKey, RSAKeyPath: *rsaKey,
		Endpoint: loadharness.Endpoint{
			Address: *server, DC: *dc, APIID: *apiID, APIHash: *apiHash, RSAKeyPath: *rsaKey,
			Obfuscated: *obfuscated, PFS: *pfs, TempKeyTTL: *tempKeyTTL,
		},
		Accounts: *accounts, ExtraDevices: *extraDevices, Concurrency: *concurrency,
		PhonePrefix: *phonePrefix, Code: code, FirstNamePrefix: *firstName,
	}
	result, err := loadharness.Provision(ctx, cfg, func(event loadharness.ProvisionEvent) {
		status := "ok"
		if event.Resumed {
			status = "resumed"
		}
		if event.Err != nil {
			status = "error"
		}
		fmt.Fprintf(os.Stdout, "provision %d/%d session=%d account=%d device=%d status=%s\n",
			event.Completed, event.Total, event.Session.Index, event.Session.AccountIndex, event.Session.DeviceIndex, status)
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "provisioned %d real MTProto sessions into %s\n", len(result.Sessions), *manifest)
	return nil
}

func runLoad(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	manifest := flags.String("manifest", filepath.FromSlash("data/loadtest/manifest.json"), "provisioned manifest")
	sessionKey := flags.String("session-key", filepath.FromSlash("data/loadtest/session.key"), "session encryption key")
	rsaOverride := flags.String("rsa-key", "", "optional RSA public/private PEM override")
	report := flags.String("report", filepath.FromSlash("data/loadtest/report.json"), "final JSON report")
	events := flags.String("events", filepath.FromSlash("data/loadtest/events.ndjson"), "periodic NDJSON evidence")
	fileFixture := flags.String("file-fixture", "", "reusable fixture JSON; empty stores beside manifest")
	serverMetrics := flags.String("server-metrics", "http://127.0.0.1:6060/metrics", "server metrics URL; empty disables")
	sessions := flags.Int("sessions", 0, "limit selected sessions; 0 uses all")
	duration := flags.Duration("duration", 30*time.Minute, "sustained load duration")
	recovery := flags.Duration("recovery", 7*time.Minute, "post-disconnect reclamation observation")
	ramp := flags.Duration("ramp", 2*time.Minute, "connection ramp duration")
	rpcInterval := flags.Duration("rpc-interval", 5*time.Second, "per-session background RPC interval")
	messageInterval := flags.Duration("message-interval", 30*time.Second, "per-primary-session message interval; negative disables")
	fileInterval := flags.Duration("file-interval", time.Minute, "per-session upload.getFile interval")
	fileSize := flags.Int("file-size", 4<<20, "generated shared download fixture bytes; 0 disables")
	fileChunk := flags.Int("file-chunk", 1<<20, "upload.getFile bytes per request (max 1MiB)")
	setupTimeout := flags.Duration("setup-timeout", 90*time.Second, "maximum first-time file fixture setup duration")
	operationTimeout := flags.Duration("operation-timeout", 30*time.Second, "maximum duration of one workload RPC")
	sampleInterval := flags.Duration("sample-interval", 10*time.Second, "evidence and server scrape interval")
	offlineFraction := flags.Float64("offline-fraction", 0.20, "fraction disconnected during offline window; 0 disables")
	offlineAt := flags.Duration("offline-at", 10*time.Minute, "offline window start from run start")
	offlineFor := flags.Duration("offline-for", 2*time.Minute, "offline window duration")
	readyRatio := flags.Float64("min-ready-ratio", 0.98, "minimum peak ready ratio")
	expectRestart := flags.Bool("expect-server-restart", false, "allow classified connection loss but require all selected sessions to reconnect")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("run accepts no positional arguments")
	}
	result, err := loadharness.Run(ctx, loadharness.RunConfig{
		ManifestPath: *manifest, SessionKeyPath: *sessionKey, RSAKeyOverride: *rsaOverride,
		ReportPath: *report, EventsPath: *events, FileFixturePath: *fileFixture, ServerMetricsURL: *serverMetrics,
		SessionLimit: *sessions, Duration: *duration, RecoveryDuration: *recovery, RampDuration: *ramp,
		RPCInterval: *rpcInterval, MessageInterval: *messageInterval, SampleInterval: *sampleInterval,
		FileInterval: *fileInterval, FileSizeBytes: *fileSize, FileChunkBytes: *fileChunk, SetupTimeout: *setupTimeout,
		OperationTimeout: *operationTimeout,
		OfflineFraction:  *offlineFraction, OfflineAt: *offlineAt, OfflineFor: *offlineFor,
		MinimumReadyRatio:   *readyRatio,
		ExpectServerRestart: *expectRestart,
	})
	if err != nil {
		return err
	}
	printSummary(result)
	if !result.Pass {
		return fmt.Errorf("load acceptance failed; see %s", *report)
	}
	return nil
}

func runSummarize(args []string) error {
	flags := flag.NewFlagSet("summarize", flag.ContinueOnError)
	path := flags.String("report", filepath.FromSlash("data/loadtest/report.json"), "JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	data, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	var report loadharness.RunReport
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		return err
	}
	printSummary(&report)
	if !report.Pass {
		return errors.New("report did not pass")
	}
	return nil
}

func printSummary(report *loadharness.RunReport) {
	fmt.Fprintf(os.Stdout, "pass=%v sessions=%d peak_ready=%d reconnects=%d disconnects=%d flood_waits=%d fatal_errors=%d\n",
		report.Pass, report.ExpectedSessions, report.PeakReadySessions, report.Reconnects, report.Disconnects,
		totalFloodWaits(report), report.WorkerFatalErrors)
	for _, failure := range report.Failures {
		fmt.Fprintln(os.Stdout, "failure:", failure)
	}
}

func totalFloodWaits(report *loadharness.RunReport) uint64 {
	var total uint64
	for _, operation := range report.Operations {
		total += operation.FloodWaits
	}
	return total
}

func usageError() error {
	return errors.New("expected one of: keygen, provision, run, summarize, help")
}

const usageText = `telesrv-load commands:
  keygen     generate an owner-only AES-256 session key
  provision  create accounts and encrypted sessions through real MTProto auth
  run        execute sustained real-client load, offline recovery and reclamation
  summarize  print the acceptance summary from a JSON report

Use "telesrv-load <command> -h" for command flags.`
