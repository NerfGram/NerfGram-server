package loadharness

import (
	"path/filepath"
	"testing"
	"time"
)

func validManifest() *Manifest {
	return &Manifest{
		Version: ManifestVersion, CreatedAt: time.Now(),
		Endpoint: Endpoint{Address: "127.0.0.1:2398", DC: 2, APIID: 1, APIHash: "hash", RSAKeyPath: "server.pem"},
		Sessions: []SessionRecord{{
			Index: 0, AccountIndex: 0, DeviceIndex: 0, Phone: "+155500000001", FirstName: "Load0001",
			SessionFile: "sessions/session-0000-device-0.bin", UserID: 1, AccessHash: 2,
		}},
	}
}

func TestManifestRoundTripContainsNoSessionSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	manifest := validManifest()
	if err := WriteManifest(path, manifest); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Sessions) != 1 || loaded.Sessions[0].UserID != 1 {
		t.Fatalf("loaded manifest = %#v", loaded)
	}
}

func TestManifestRejectsEscapingAndDuplicateSessionPaths(t *testing.T) {
	manifest := validManifest()
	manifest.Sessions[0].SessionFile = "../outside.bin"
	if err := manifest.Validate(); err == nil {
		t.Fatal("escaping session path accepted")
	}
	manifest = validManifest()
	duplicate := manifest.Sessions[0]
	duplicate.Index = 1
	duplicate.AccountIndex = 1
	duplicate.UserID = 2
	manifest.Sessions = append(manifest.Sessions, duplicate)
	if err := manifest.Validate(); err == nil {
		t.Fatal("duplicate session path accepted")
	}
}

func TestExplicitZeroExtraDevicesAndRecoveryAreValid(t *testing.T) {
	provision := ProvisionConfig{
		ManifestPath: "manifest.json", SessionKeyPath: "key", RSAKeyPath: "rsa",
		Endpoint: *&validManifest().Endpoint, Accounts: 1, ExtraDevices: 0, Concurrency: 1,
		PhonePrefix: "+155500", Code: "12345", FirstNamePrefix: "Load",
	}
	if err := provision.validate(); err != nil {
		t.Fatalf("zero extra devices: %v", err)
	}
	run := RunConfig{
		ManifestPath: "manifest.json", SessionKeyPath: "key", ReportPath: "report.json",
		Duration: time.Second, RecoveryDuration: 0, RampDuration: 0,
		RPCInterval: time.Millisecond, MessageInterval: -1, SampleInterval: time.Millisecond,
		OperationTimeout: time.Second, MinimumReadyRatio: 1,
	}
	if err := run.validate(); err != nil {
		t.Fatalf("zero recovery/ramp: %v", err)
	}
	run.OperationTimeout = 0
	if err := run.validate(); err == nil {
		t.Fatal("zero operation timeout accepted")
	}
}
