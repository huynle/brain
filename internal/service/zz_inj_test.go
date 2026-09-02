package service

import (
	"os"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestZZAutomationInjection(t *testing.T) {
	auto := buildSimpleFeatureCheckoutScript(BuiltInFeatureCheckoutSimpleConfig{
		MergeTargetBranch:  "main",
		RemoteBranchPolicy: "delete",
	})
	evt := types.Event{
		ProjectID: "p",
		FeatureID: `x'; touch /tmp/PWNED_AUTOMATION; echo '`,
	}
	out := renderAutomationTemplate(auto, "p", evt)
	os.WriteFile("/tmp/fc_inj_auto.sh", []byte(out), 0o755)
	t.Logf("first lines:\n%.400s", out)
}
