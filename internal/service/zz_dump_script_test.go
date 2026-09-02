package service

import (
	"os"
	"testing"
)

func TestZZDumpScripts(t *testing.T) {
	auto := buildSimpleFeatureCheckoutScript(BuiltInFeatureCheckoutSimpleConfig{
		MergeTargetBranch:  "main",
		RemoteBranchPolicy: "delete",
	})
	os.WriteFile("/tmp/fc_auto.sh", []byte(auto), 0o755)

	man := renderSimpleFeatureCheckoutScript(simpleCheckoutScriptParams{
		FeatureExpr:  shellSingleQuoted("feat/my-thing"),
		ProjectExpr:  shellSingleQuoted("brain-api"),
		SourceBranch: safeBranchLiteral("feat/my-thing"),
		TargetBranch: "main",
		RemoteDelete: false,
	})
	os.WriteFile("/tmp/fc_manual.sh", []byte(man), 0o755)

	// injection probe on manual path
	inj := renderSimpleFeatureCheckoutScript(simpleCheckoutScriptParams{
		FeatureExpr:  shellSingleQuoted("a'; touch /tmp/PWNED_MANUAL; echo '"),
		ProjectExpr:  shellSingleQuoted("p"),
		SourceBranch: safeBranchLiteral("a'; touch /tmp/PWNED_BRANCH; echo '"),
		TargetBranch: "main",
		RemoteDelete: false,
	})
	os.WriteFile("/tmp/fc_inj_manual.sh", []byte(inj), 0o755)
	t.Logf("auto len=%d manual len=%d", len(auto), len(man))
}
