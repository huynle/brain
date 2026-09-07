package runner

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestGitHostAdvertisements(t *testing.T) {
	t.Setenv("PHASE2_HOST_TOKEN", "dummy-secret")
	client := newMockClient()
	tr := newTestRunner(client, newMockExecutor(), newMockProcessMgr(), newMockStateMgr())
	tr.config.GitToken = ""
	tr.config.GitTokenEnv = ""
	tr.config.GitHostTokenEnv = map[string]string{"Supported.invalid:443": "PHASE2_HOST_TOKEN"}
	tr.config.Capabilities = []string{"docker", "git-credential-host:spoof.invalid"}
	tr.registerWithAPI(context.Background())
	want := []string{"docker", "git-credential-host:supported.invalid"}
	if got := client.registerCalls[0].Capabilities; !reflect.DeepEqual(got, want) {
		t.Errorf("registration capabilities = %v, want %v", got, want)
	}
	tr.sendHeartbeat(context.Background())
	assertHeartbeatCapabilities(t, client.heartbeatCalls[0].Request, want)
	// Loss of credentials and explicit deny must replace, not preserve, old ads.
	t.Setenv("PHASE2_HOST_TOKEN", "")
	tr.sendHeartbeat(context.Background())
	assertHeartbeatCapabilities(t, client.heartbeatCalls[1].Request, []string{"docker"})
	tr.config.Capabilities = nil
	tr.config.GitToken = "legacy"
	tr.config.GitAllowedHosts = []string{}
	tr.sendHeartbeat(context.Background())
	assertHeartbeatCapabilities(t, client.heartbeatCalls[2].Request, []string{})
	// Invalid config revokes support rather than emitting a stale advertisement.
	tr.config.GitAllowedHosts = []string{"bad/host"}
	tr.sendHeartbeat(context.Background())
	assertHeartbeatCapabilities(t, client.heartbeatCalls[3].Request, []string{})
}

func assertHeartbeatCapabilities(t *testing.T, request interface{}, want []string) {
	t.Helper()
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Capabilities, want) {
		t.Errorf("heartbeat capabilities = %#v, want %#v (wire %s)", decoded.Capabilities, want, data)
	}
}
