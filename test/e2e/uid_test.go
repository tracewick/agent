//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	spoolOwnerUID = "33"
	spoolOwnerGID = "33"
	wrongAgentUID = "1000"
	wrongAgentGID = "1000"
	busyboxImage  = "busybox:1.36"
)

// TestUIDMismatchCannotClaimMatchingCan mirrors deploy/ UID contract: spool
// owned by PHP-like UID 33 mode 0700/0600. A different non-root agent UID must
// leave the file unclaimed; the matching UID delivers and acks.
//
// Uses Docker named volumes (no host bind mounts) so DAC is real on Linux CI and
// Docker Desktop. Skips when docker is unavailable.
func TestUIDMismatchCannotClaimMatchingCan(t *testing.T) {
	requireDocker(t)
	hostBin := linuxAgentBinary(t)

	var posts int
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts++
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	pyroscopeURL := dockerReachableURL(t, ln.Addr().(*net.TCPAddr).Port)
	logf(t, "uid: httptest %s (container URL %s)", srv.URL, pyroscopeURL)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	spoolVol := "tracewick-e2e-uid-spool-" + suffix
	binVol := "tracewick-e2e-uid-bin-" + suffix
	for _, vol := range []string{spoolVol, binVol} {
		if out, err := dockerRun(t, 30*time.Second, nil, "volume", "create", vol); err != nil {
			t.Fatalf("volume create %s: %v\n%s", vol, err, out)
		}
		vol := vol
		t.Cleanup(func() { _, _ = dockerRun(t, 30*time.Second, nil, "volume", "rm", "-f", vol) })
	}

	seedName := formatSpoolName(1)
	logf(t, "uid: seeding fixture into volume as %s:%s", spoolOwnerUID, spoolOwnerGID)
	seedScript := fmt.Sprintf(
		"mkdir -p /spool && cat > /spool/%s && chown -R %s:%s /spool && chmod 0700 /spool && chmod 0600 /spool/%s && ls -la /spool",
		seedName, spoolOwnerUID, spoolOwnerGID, seedName,
	)
	seedOut, err := dockerRun(t, 60*time.Second, fixtureBytes(t, "web-request.spool"),
		"run", "--rm", "-i",
		"-v", spoolVol+":/spool",
		busyboxImage,
		"sh", "-c", seedScript,
	)
	if err != nil {
		t.Fatalf("seed spool: %v\n%s", err, seedOut)
	}
	logf(t, "uid: seeded\n%s", bytesTrim(seedOut))

	logf(t, "uid: copying agent binary into volume (docker cp, no bind mount)")
	copyHostFileToVolume(t, hostBin, binVol, "tracewick-agent")

	logf(t, "uid: agent as %s:%s (mismatch) — expect leftovers", wrongAgentUID, wrongAgentGID)
	out, err := runAgentInDocker(t, binVol, spoolVol, wrongAgentUID, wrongAgentGID, pyroscopeURL, "e2e-uid-wrong")
	if err != nil {
		logf(t, "uid: mismatch run err=%v out=%s", err, trimLog(string(out), 400))
	} else {
		logf(t, "uid: mismatch run ok; %s", trimLog(string(out), 400))
	}
	left := listDockerSpool(t, spoolVol)
	if len(left) == 0 {
		t.Fatalf("mismatch UID must not ack spool; leftovers empty; posts=%d\n%s", posts, out)
	}
	logf(t, "uid: mismatch leftovers=%v posts=%d", left, posts)
	if posts != 0 {
		t.Fatalf("mismatch UID must not push; posts=%d", posts)
	}

	logf(t, "uid: agent as %s:%s (match) — expect deliver+ack", spoolOwnerUID, spoolOwnerGID)
	out, err = runAgentInDocker(t, binVol, spoolVol, spoolOwnerUID, spoolOwnerGID, pyroscopeURL, "e2e-uid-match")
	if err != nil {
		t.Fatalf("matching UID agent: %v\n%s", err, out)
	}
	logf(t, "uid: match run ok; %s", trimLog(string(out), 400))
	left = listDockerSpool(t, spoolVol)
	if len(left) != 0 {
		t.Fatalf("matching UID leftovers=%v posts=%d\n%s", left, posts, out)
	}
	if posts < 1 {
		t.Fatalf("matching UID posts=%d want ≥1", posts)
	}
	logf(t, "uid: OK mismatch blocked + match delivered posts=%d", posts)
}

func requireDocker(t *testing.T) {
	t.Helper()
	out, err := dockerRun(t, 10*time.Second, nil, "version", "--format", "{{.Server.Version}}")
	if err != nil {
		t.Skipf("docker unavailable: %v (%s)", err, bytesTrim(out))
	}
	logf(t, "docker server %s", bytesTrim(out))
	// Ensure the image exists (CI runners are clean; --pull=never would skip the suite).
	if pullOut, pullErr := dockerRun(t, 2*time.Minute, nil, "pull", busyboxImage); pullErr != nil {
		t.Skipf("docker pull %s: %v (%s)", busyboxImage, pullErr, bytesTrim(pullOut))
	}
	// docker version/pull can succeed while `docker run` is wedged (Desktop glitches).
	probe, err := dockerRun(t, 30*time.Second, nil, "run", "--rm", "--pull=never", busyboxImage, "/bin/echo", "ok")
	if err != nil {
		t.Skipf("docker run unavailable (restart Docker Desktop?): %v (%s)", err, bytesTrim(probe))
	}
}

// linuxAgentBinary cross-compiles the agent for the Docker Engine OS/arch so the
// binary can run inside a Linux container (host agentBinary is darwin on macOS).
func linuxAgentBinary(t *testing.T) string {
	t.Helper()
	archOut, err := dockerRun(t, 10*time.Second, nil, "version", "--format", "{{.Server.Arch}}")
	if err != nil {
		t.Fatalf("docker arch: %v\n%s", err, archOut)
	}
	goarch := dockerArchToGOARCH(bytesTrim(archOut))
	root := repoRoot(t)
	dir := t.TempDir()
	bin := dir + "/tracewick-agent"
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/tracewick-agent") //nolint:gosec // test
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+goarch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("linux agent build (%s): %v\n%s", goarch, err, out)
	}
	logf(t, "linux agent binary %s (GOARCH=%s)", bin, goarch)
	return bin
}

func dockerArchToGOARCH(arch string) string {
	switch strings.ToLower(arch) {
	case "aarch64", "arm64":
		return "arm64"
	case "x86_64", "amd64":
		return "amd64"
	default:
		return "amd64"
	}
}

func dockerReachableURL(t *testing.T, port int) string {
	t.Helper()
	if runtime.GOOS == "linux" {
		return fmt.Sprintf("http://127.0.0.1:%d", port)
	}
	return fmt.Sprintf("http://host.docker.internal:%d", port)
}

func dockerRun(t *testing.T, timeout time.Duration, stdin []byte, args ...string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...) //nolint:gosec // test harness
	cmd.Env = os.Environ()
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	return cmd.CombinedOutput()
}

func copyHostFileToVolume(t *testing.T, hostPath, volume, destName string) {
	t.Helper()
	name := "tracewick-e2e-cp-" + fmt.Sprintf("%d", time.Now().UnixNano())
	if out, err := dockerRun(t, 60*time.Second, nil,
		"create", "--name", name, "-v", volume+":/out", busyboxImage, "true",
	); err != nil {
		t.Fatalf("docker create: %v\n%s", err, out)
	}
	t.Cleanup(func() { _, _ = dockerRun(t, 30*time.Second, nil, "rm", "-f", name) })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cp := exec.CommandContext(ctx, "docker", "cp", hostPath, name+":/out/"+destName) //nolint:gosec // test
	if out, err := cp.CombinedOutput(); err != nil {
		t.Fatalf("docker cp: %v\n%s", err, out)
	}
	if out, err := dockerRun(t, 30*time.Second, nil, "rm", "-f", name); err != nil {
		t.Fatalf("docker rm helper: %v\n%s", err, out)
	}
	if out, err := dockerRun(t, 30*time.Second, nil,
		"run", "--rm", "-v", volume+":/out", busyboxImage,
		"sh", "-c", "chmod 0755 /out/"+destName+" && ls -la /out/"+destName,
	); err != nil {
		t.Fatalf("chmod binary in volume: %v\n%s", err, out)
	}
}

func runAgentInDocker(t *testing.T, binVol, spoolVol, uid, gid, pyroscopeURL, readerID string) ([]byte, error) {
	t.Helper()
	args := []string{
		"run", "--rm",
		"--user", uid + ":" + gid,
		"-v", spoolVol + ":/spool",
		"-v", binVol + ":/agent:ro",
	}
	if runtime.GOOS == "linux" {
		args = append(args, "--network", "host")
	} else {
		args = append(args, "--add-host", "host.docker.internal:host-gateway")
	}
	args = append(args,
		busyboxImage,
		"/agent/tracewick-agent",
		"run",
		"--once",
		"--spool-dir=/spool",
		"--pyroscope-url="+pyroscopeURL,
		"--reader-id="+readerID,
		"--sleep=50ms",
	)
	logf(t, "docker %s", strings.Join(args, " "))
	return dockerRun(t, 2*time.Minute, nil, args...)
}

func listDockerSpool(t *testing.T, volume string) []string {
	t.Helper()
	out, err := dockerRun(t, 60*time.Second, nil,
		"run", "--rm",
		"-v", volume+":/spool",
		busyboxImage,
		"sh", "-c", "ls -1 /spool 2>/dev/null || true",
	)
	if err != nil {
		t.Fatalf("list spool volume: %v\n%s", err, out)
	}
	var left []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "rejected" {
			continue
		}
		if strings.HasSuffix(line, ".spool") || strings.Contains(line, ".spool.claimed-") {
			left = append(left, line)
		}
	}
	return left
}

func bytesTrim(b []byte) string { return strings.TrimSpace(string(b)) }
