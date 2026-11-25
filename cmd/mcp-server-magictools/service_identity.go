package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
	"github.com/maccavelli/mcp-server-magictools/internal/serviceidentity"
)

type processIdentity struct {
	PID        int
	Start      uint64
	Executable string
}
type identityReader interface {
	Self() (processIdentity, error)
	Verify(processIdentity) (bool, error)
}
type systemIdentityReader struct{}

func (systemIdentityReader) Self() (processIdentity, error) {
	self, err := serviceidentity.Self()
	if err != nil {
		return processIdentity{}, err
	}
	bin, err := os.Executable()
	if err != nil {
		return processIdentity{}, err
	}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		bin = resolved
	}
	return processIdentity{PID: self.PID, Start: self.Start, Executable: bin}, nil
}
func (systemIdentityReader) Verify(id processIdentity) (bool, error) {
	return serviceidentity.Verify(serviceidentity.Identity{PID: id.PID, Start: id.Start, Executable: id.Executable})
}

var errStaleIdentity = errors.New("state process identity is stale or reused")

var serviceIdentity identityReader = systemIdentityReader{}
var serviceExpectedExecutable = expectedServiceExecutable
var serviceStateFile = serviceStatePath
var serviceHTTPClient = &http.Client{Timeout: 3 * time.Second}

func currentServiceIdentity() (processIdentity, error) { return serviceIdentity.Self() }

func expectedServiceExecutable() (string, error) {
	if runtime.GOOS == goOSDarwin {
		path, err := launchdPlistPath()
		if err != nil {
			return "", err
		}
		program, err := launchdDefinitionProgram(path)
		if err != nil {
			return "", err
		}
		return program, nil
	}
	bin, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(bin)
	if err == nil {
		bin = resolved
	}
	return filepath.Abs(bin)
}

func readExpectedServiceState(expected processIdentity) (cliServiceState, error) {
	data, err := os.ReadFile(serviceStateFile())
	if err != nil {
		return cliServiceState{}, err
	}
	var st cliServiceState
	if err := json.Unmarshal(data, &st); err != nil {
		return st, err
	}
	if st.PID <= 0 || st.StartToken == 0 {
		return st, errors.New("state has no process start token")
	}
	if st.PID != expected.PID && expected.PID != 0 {
		return st, fmt.Errorf("state pid %d does not match manager pid %d", st.PID, expected.PID)
	}
	if expected.Executable != "" {
		want, err := filepath.EvalSymlinks(expected.Executable)
		if err == nil {
			expected.Executable = want
		}
		got, err := filepath.EvalSymlinks(st.BinaryPath)
		if err == nil {
			st.BinaryPath = got
		}
		if st.BinaryPath != expected.Executable {
			return st, fmt.Errorf("state binary %q is not expected %q", st.BinaryPath, expected.Executable)
		}
	}
	ok, err := serviceIdentity.Verify(processIdentity{PID: st.PID, Start: st.StartToken, Executable: st.BinaryPath})
	if err != nil {
		return st, err
	}
	if !ok {
		return st, errStaleIdentity
	}
	return st, nil
}

func probeServiceHealth(ctx context.Context) (service.Health, error) {
	expected, err := serviceExpectedExecutable()
	if err != nil {
		return service.Health{}, err
	}
	st, err := readExpectedServiceState(processIdentity{Executable: expected})
	if err != nil {
		return service.Health{Detail: err.Error()}, nil //nolint:nilerr // an unhealthy observation is not a probe transport failure
	}
	h, err := serviceHealthHTTP(ctx, st.Addr)
	if err != nil {
		return service.Health{Detail: err.Error()}, nil //nolint:nilerr // polling continues while the endpoint is unavailable
	}
	h.Instance = fmt.Sprintf("%d@%d", st.PID, st.StartToken)
	return h, nil
}

func serviceHealthHTTP(ctx context.Context, addr string) (service.Health, error) {
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil {
		return service.Health{}, err
	}
	if u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return service.Health{}, fmt.Errorf("refuse non-loopback health address %q", addr)
	}
	u.Path, u.RawQuery, u.Fragment = "/health", "", ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return service.Health{}, err
	}
	resp, err := serviceHTTPClient.Do(req)
	if err != nil {
		return service.Health{}, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("service health: close response body", "error", err)
		}
	}()
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return service.Health{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return service.Health{Detail: fmt.Sprintf("health HTTP %d (%s)", resp.StatusCode, body.Status)}, nil
	}
	if body.Status != "ok" && body.Status != healthDegraded {
		return service.Health{Detail: "unexpected health status " + body.Status}, nil
	}
	return service.Health{Ready: true, Detail: body.Status}, nil
}
