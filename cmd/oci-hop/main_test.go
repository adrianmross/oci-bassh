package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var requiredKeys = map[string][]string{
	"oci-hop-doctor.schema.json":         {"ok", "tools", "versions", "oci_context", "bastion_doctor", "targets"},
	"oci-hop-check.schema.json":          {"ok", "tools", "versions", "oci_context", "bastion_doctor", "targets"},
	"oci-hop-inspect.schema.json":        {"ok", "host", "versions", "oci_status", "auth", "bastion_doctor", "ssh_config", "ssh_effective"},
	"oci-hop-repair.schema.json":         {"ok", "host", "repair", "ensure_requested", "connect_command"},
	"oci-hop-ensure.schema.json":         {"ok", "host", "auth", "ensure", "ssh_config", "connect_command"},
	"oci-hop-track.schema.json":          {"ok", "host", "track", "target"},
	"oci-hop-ssh.schema.json":            {"ok", "host", "auth", "ensure", "ssh_command"},
	"oci-hop-contract-check.schema.json": {"ok", "checks"},
	"oci-hop-explain.schema.json":        {"ok", "host", "explain"},
	"oci-hop-paths.schema.json":          {"ok", "paths"},
	"oci-hop-upgrade.schema.json":        {"ok", "dry_run", "command"},
	"oci-hop-version.schema.json":        {"ok", "version", "commit", "date"},
}

type commandRun struct {
	stdout string
	stderr string
	code   int
}

func TestSchemasAreValidJSON(t *testing.T) {
	for _, path := range mustGlob(t, filepath.Join(repoRoot(t), "schemas", "*.schema.json")) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatalf("schema is not valid JSON: %v", err)
			}
		})
	}
}

func TestHermeticCLIContract(t *testing.T) {
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binary := buildOCIHop(t, tmp)
	writeHermeticShims(t, binDir)
	env := append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	helper := []string{binary}

	checks := []struct {
		schema string
		args   []string
	}{
		{"oci-hop-doctor.schema.json", append(helper, "doctor")},
		{"oci-hop-check.schema.json", append(helper, "check")},
		{"oci-hop-inspect.schema.json", append(helper, "inspect", "my-vps-01")},
		{"oci-hop-repair.schema.json", append(helper, "repair", "my-vps-01")},
		{"oci-hop-repair.schema.json", append(helper, "repair", "--ensure", "my-vps-01")},
		{"oci-hop-ensure.schema.json", append(helper, "ensure-target", "my-vps-01")},
		{"oci-hop-ensure.schema.json", append(helper, "ensure", "my-vps-01")},
		{"oci-hop-track.schema.json", append(helper, "track-from-terraform", "my-vps-01", tmp)},
		{"oci-hop-track.schema.json", append(helper, "track", "my-vps-01", tmp)},
		{"oci-hop-track.schema.json", append(helper, "track", "my-vps-01", "--terraform-dir", tmp)},
		{"oci-hop-ssh.schema.json", append(helper, "ssh", "--dry-run", "my-vps-01")},
		{"oci-hop-ssh.schema.json", append(helper, "ssh", "--dry-run", "my-vps-01", "-p", "2222")},
		{"oci-hop-explain.schema.json", append(helper, "explain", "my-vps-01")},
		{"oci-hop-paths.schema.json", append(helper, "paths", "-o", "json")},
		{"oci-hop-upgrade.schema.json", append(helper, "upgrade")},
		{"oci-hop-version.schema.json", append(helper, "version", "-o", "json")},
		{"oci-hop-version.schema.json", append(helper, "--version", "--json")},
		{"oci-hop-contract-check.schema.json", append(helper, "contract-check")},
	}

	for _, check := range checks {
		t.Run(strings.Join(check.args[1:], "_"), func(t *testing.T) {
			run := runCommandForTest(t, check.args, env)
			if run.code != 0 {
				t.Fatalf("command failed with %d\nstdout:\n%s\nstderr:\n%s", run.code, run.stdout, run.stderr)
			}
			payload := decodeObject(t, run.stdout)
			assertRequiredKeys(t, check.schema, payload)
		})
	}

	ready := runCommandForTest(t, append(helper, "my-vps-01"), env)
	if ready.code != 0 {
		t.Fatalf("hop host failed with %d\nstdout:\n%s\nstderr:\n%s", ready.code, ready.stdout, ready.stderr)
	}
	if got, want := strings.TrimSpace(ready.stdout), "ready  my-vps-01  10.0.1.25  via my-bastion"; got != want {
		t.Fatalf("unexpected compact ready line\nwant: %q\n got: %q", want, got)
	}
	for _, want := range []string{
		"checking OCI auth...",
		"ensuring Bastion session for my-vps-01 (timeout 2m)...",
		"refreshing SSH config for my-vps-01...",
	} {
		if !strings.Contains(ready.stderr, want) {
			t.Fatalf("ready stderr missing %q\nstderr:\n%s", want, ready.stderr)
		}
	}

	readyShort := runCommandForTest(t, append(helper, "--wait-timeout", "15s", "my-vps-01"), env)
	if readyShort.code != 0 {
		t.Fatalf("hop host with wait timeout failed with %d\nstdout:\n%s\nstderr:\n%s", readyShort.code, readyShort.stdout, readyShort.stderr)
	}
	if !strings.Contains(readyShort.stderr, "ensuring Bastion session for my-vps-01 (timeout 15s)...") {
		t.Fatalf("custom timeout missing from progress\nstderr:\n%s", readyShort.stderr)
	}

	for _, args := range [][]string{
		append(helper, "--wait-timeout", "3m", "ensure", "my-vps-01"),
		append(helper, "ensure", "my-vps-01", "--wait-timeout", "3m"),
	} {
		run := runCommandForTest(t, args, env)
		if run.code != 0 {
			t.Fatalf("ensure with wait timeout failed with %d\nargs: %v\nstdout:\n%s\nstderr:\n%s", run.code, args, run.stdout, run.stderr)
		}
		payload := decodeObject(t, run.stdout)
		ensure, ok := payload["ensure"].(map[string]any)
		if !ok {
			t.Fatalf("ensure payload missing nested ensure command: %#v", payload)
		}
		command, ok := ensure["command"].([]any)
		if !ok {
			t.Fatalf("nested ensure command missing: %#v", ensure)
		}
		if got := strings.Join(jsonStringSlice(t, command), " "); !strings.Contains(got, "--wait-timeout 3m") {
			t.Fatalf("nested ensure command did not include custom timeout\ncommand: %s", got)
		}
	}

	sshRun := runCommandForTest(t, append(helper, "ssh", "my-vps-01"), env)
	if sshRun.code != 0 {
		t.Fatalf("ssh fallback command failed with %d\nstdout:\n%s\nstderr:\n%s", sshRun.code, sshRun.stdout, sshRun.stderr)
	}
	if got, want := strings.TrimSpace(sshRun.stdout), "connected my-vps-01"; got != want {
		t.Fatalf("unexpected ssh fallback output\nwant: %q\n got: %q", want, got)
	}

	setupRun := runCommandForTest(t, append(helper, "setup", "shell"), env)
	if setupRun.code != 0 {
		t.Fatalf("setup shell failed with %d\nstdout:\n%s\nstderr:\n%s", setupRun.code, setupRun.stdout, setupRun.stderr)
	}
	for _, want := range []string{"hop vmordws02", "oci-hop ssh vmordws02", "hssh()"} {
		if !strings.Contains(setupRun.stdout, want) {
			t.Fatalf("setup shell output missing %q\nstdout:\n%s", want, setupRun.stdout)
		}
	}

	setupPath := filepath.Join(tmp, "oci-hop.zsh")
	setupInstall := runCommandForTest(t, append(helper, "setup", "shell", "--install", "--out", setupPath), env)
	if setupInstall.code != 0 {
		t.Fatalf("setup shell --install failed with %d\nstdout:\n%s\nstderr:\n%s", setupInstall.code, setupInstall.stdout, setupInstall.stderr)
	}
	if data, err := os.ReadFile(setupPath); err != nil || !strings.Contains(string(data), "hssh()") {
		t.Fatalf("setup shell did not write expected file: err=%v data=%q", err, data)
	}

	readyJSON := runCommandForTest(t, append(helper, "-o", "json", "my-vps-01"), env)
	if readyJSON.code != 0 {
		t.Fatalf("hop host json failed with %d\nstdout:\n%s\nstderr:\n%s", readyJSON.code, readyJSON.stdout, readyJSON.stderr)
	}
	if strings.TrimSpace(readyJSON.stderr) != "" {
		t.Fatalf("json output should not include progress on stderr:\n%s", readyJSON.stderr)
	}
	readyPayload := decodeObject(t, readyJSON.stdout)
	for _, key := range []string{"ok", "host", "auth", "ensure", "ssh_config"} {
		if _, ok := readyPayload[key]; !ok {
			t.Fatalf("ready json missing %q in %#v", key, readyPayload)
		}
	}

	link := filepath.Join(tmp, "hop")
	if err := os.Symlink(binary, link); err != nil {
		t.Fatal(err)
	}
	run := runCommandForTest(t, []string{link, "version", "-o", "json"}, env)
	if run.code != 0 {
		t.Fatalf("hop command failed with %d\nstdout:\n%s\nstderr:\n%s", run.code, run.stdout, run.stderr)
	}
	assertRequiredKeys(t, "oci-hop-version.schema.json", decodeObject(t, run.stdout))

	failEnv := append([]string{}, env...)
	failEnv = append(failEnv, "BASTION_SESSION_FAIL_DOCTOR=1")
	doctor := runCommandForTest(t, append(helper, "doctor", "my-vps-01"), failEnv)
	if doctor.code != 0 {
		t.Fatalf("doctor should report unhealthy diagnostics without failing, got %d\nstdout:\n%s\nstderr:\n%s", doctor.code, doctor.stdout, doctor.stderr)
	}
	doctorPayload := decodeObject(t, doctor.stdout)
	assertRequiredKeys(t, "oci-hop-doctor.schema.json", doctorPayload)
	if doctorPayload["ok"] != false || doctorPayload["issue"] == nil {
		t.Fatalf("doctor failure payload should include ok=false and issue, got %#v", doctorPayload)
	}
	check := runCommandForTest(t, append(helper, "check", "my-vps-01"), failEnv)
	if check.code == 0 {
		t.Fatalf("check should fail when diagnostics are unhealthy\nstdout:\n%s", check.stdout)
	}
	checkPayload := decodeObject(t, check.stdout)
	assertRequiredKeys(t, "oci-hop-check.schema.json", checkPayload)
}

func buildOCIHop(t *testing.T, tmp string) string {
	t.Helper()
	out := filepath.Join(tmp, "oci-hop")
	cmd := exec.Command("go", "build", "-o", out, "./cmd/oci-hop")
	cmd.Dir = repoRoot(t)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, string(output))
	}
	return out
}

func writeHermeticShims(t *testing.T, binDir string) {
	t.Helper()
	writeExecutable(t, filepath.Join(binDir, "oci-context"), `#!/bin/sh
set -eu
case "$*" in
  "auth ensure --output json")
    printf '{"ok":true,"state":"ready","context":"dev","profile":"DEFAULT","auth_method":"api_key"}\n'
    ;;
  "status --cached -o json")
    printf '{"context":"dev","profile":"DEFAULT","region":"us-phoenix-1","auth_method":"api_key"}\n'
    ;;
  "doctor -o json")
    printf '{"auth_ensure":{"ok":true,"state":"ready"},"current_context":"dev"}\n'
    ;;
  "auth show --output json")
    printf '{"context":"dev","daemon_available":false}\n'
    ;;
  "--version"|"-v"|"version")
    printf '0.99.0\n'
    ;;
  *)
    printf 'unexpected oci-context %s\n' "$*" >&2
    exit 2
    ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "bastion-session"), `#!/bin/sh
set -eu
host="${2:-my-vps-01}"
case "$*" in
  "--version"|"-v"|"version")
    printf '0.99.0\n'
    ;;
  "target list -o json")
    printf '[{"name":"my-vps-01"}]\n'
    ;;
  "doctor -o json"|"doctor my-vps-01 -o json"|"doctor my-vps-01 --cached -o json"|"doctor my-vps-01 --fix -o json")
    if [ "${BASTION_SESSION_FAIL_DOCTOR:-}" ]; then
      printf '{"ok":false,"issues":[{"message":"broken fixture"}]}\n'
      printf 'broken fixture\n' >&2
      exit 1
    fi
    printf '{"current_bastion":{"available":true},"session":{"cached":{"lifecycle":"ACTIVE"}},"ssh_include":{"exists":true}}\n'
    ;;
  "target import my-vps-01 --terraform-outputs "*)
    printf '{"tracked":true,"host":"my-vps-01"}\n'
    ;;
  "target show my-vps-01 -o json")
    printf '{"name":"my-vps-01","instance_id":"ocid1.instance","private_ip":"10.0.1.25"}\n'
    ;;
  "ensure my-vps-01 -o json"|"ensure my-vps-01 -o json --wait-timeout 2m"|"ensure my-vps-01 -o json --wait-timeout 15s"|"ensure my-vps-01 -o json --wait-timeout 3m")
    printf '{"ready":true,"ssh_host":"my-vps-01","connect_command":"ssh my-vps-01","target_private_ip":"10.0.1.25"}\n'
    ;;
  "explain my-vps-01 -o json")
    printf '{"host":"my-vps-01","target":"10.0.1.25","proxyjump":"my-bastion","connect_command":"ssh my-vps-01"}\n'
    ;;
  "ssh-config show my-vps-01 -o json")
    printf '{"host":"my-vps-01","hostname":"10.0.1.25","user":"cloud-user","proxyjump":"my-bastion"}\n'
    ;;
  status*)
    printf '{"session_id":"ocid1.session","lifecycle":"ACTIVE"}\n'
    ;;
  *)
    printf 'unexpected bastion-session %s\n' "$*" >&2
    exit 2
    ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "ssh"), `#!/bin/sh
set -eu
case "$*" in
  "my-vps-01")
    printf 'connected my-vps-01\n'
    ;;
  "-G my-vps-01")
    printf 'user cloud-user\n'
    printf 'hostname 10.0.1.25\n'
    printf 'proxyjump my-bastion\n'
    ;;
  "-V")
    printf 'OpenSSH_9.9\n'
    ;;
  *)
    printf 'unexpected ssh %s\n' "$*" >&2
    exit 2
    ;;
esac
`)
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func runCommandForTest(t *testing.T, args []string, env []string) commandRun {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = env
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return commandRun{stdout: stdout.String(), stderr: stderr.String()}
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return commandRun{stdout: stdout.String(), stderr: stderr.String(), code: exitErr.ExitCode()}
	}
	t.Fatalf("command failed before exit: %v", err)
	return commandRun{}
}

func decodeObject(t *testing.T, raw string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("output is not a JSON object: %v\n%s", err, raw)
	}
	return payload
}

func jsonStringSlice(t *testing.T, values []any) []string {
	t.Helper()
	out := make([]string, len(values))
	for i, value := range values {
		s, ok := value.(string)
		if !ok {
			t.Fatalf("expected string at index %d, got %T", i, value)
		}
		out[i] = s
	}
	return out
}

func assertRequiredKeys(t *testing.T, schema string, payload map[string]any) {
	t.Helper()
	for _, key := range requiredKeys[schema] {
		if _, ok := payload[key]; !ok {
			t.Fatalf("%s: missing required key %q in %#v", schema, key, payload)
		}
	}
	if ok, exists := payload["ok"]; exists {
		if _, isBool := ok.(bool); !isBool {
			t.Fatalf("%s: ok must be boolean, got %T", schema, ok)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func mustGlob(t *testing.T, pattern string) []string {
	t.Helper()
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatalf("no matches for %s", pattern)
	}
	return matches
}
