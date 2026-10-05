//go:build darwin

package scriptexec

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

const darwinPrototypeProfile = `(version 1)
(deny default)
(allow process-exec (literal (param "NODE")))
(allow sysctl-read)
(allow file-read-metadata)
(allow file-read* (literal "/") (literal "/dev/null") (literal (param "NODE"))
 (subpath "/usr/lib") (subpath "/System/Library")
 (regex #"^/opt/homebrew/Cellar/[^/]+/[^/]+/lib/.*[.]dylib$"))
(allow file-map-executable)
`

func darwinPrototypeCommand(t *testing.T, ctx context.Context, script string, args ...string) *exec.Cmd {
	t.Helper()
	if os.Getenv("BRAIN_SCRIPT_CONFINEMENT_PROTOTYPE") != "1" {
		t.Skip("opt-in macOS confinement experiment, not a release gate")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	node, err = filepath.EvalSymlinks(node)
	if err != nil {
		t.Fatal(err)
	}
	commandArgs := []string{"-D", "NODE=" + node, "-p", darwinPrototypeProfile, node, "--openssl-config=/dev/null", "--max-old-space-size=16", "-e", script}
	commandArgs = append(commandArgs, args...)
	cmd := exec.CommandContext(ctx, "/usr/bin/sandbox-exec", commandArgs...)
	cmd.Env = []string{}
	cmd.Dir = t.TempDir()
	cmd.WaitDelay = time.Second
	return cmd
}

// This opt-in experiment is NOT a production launcher or a chosen runtime.
// It measures a subset of host denials using installed Node as an adversarial
// process. It does not establish memory/CPU accounting or hosted isolation.
func TestDarwinConfinementPrototype(t *testing.T) {
	root := t.TempDir()
	secret := filepath.Join(root, "host-secret")
	if err := os.WriteFile(secret, []byte("must-not-be-readable"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	script := `const fs=require('node:fs');const net=require('node:net');const cp=require('node:child_process');
(async()=>{
 const out={asyncResult:await Promise.resolve(42),environment:Object.keys(process.env).length};
 try{fs.readFileSync(process.argv[1]);out.hostRead='ALLOWED'}catch(e){out.hostRead=e.code}
 try{fs.writeFileSync(process.argv[1]+'.write','x');out.hostWrite='ALLOWED'}catch(e){out.hostWrite=e.code}
 const child=cp.spawnSync('/bin/echo',['forbidden']);out.spawn=child.error?.code??'ALLOWED';
 out.network=await new Promise(resolve=>{const s=net.connect({host:'127.0.0.1',port:Number(process.argv[2])});s.on('connect',()=>{s.destroy();resolve('ALLOWED')});s.on('error',e=>resolve(e.code));});
 console.log(JSON.stringify(out));
})().catch(()=>process.exit(2));`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := darwinPrototypeCommand(t, ctx, script, secret, strconv.Itoa(port))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("prototype cannot launch: %v, output=%s", err, out)
	}
	var result struct {
		AsyncResult int    `json:"asyncResult"`
		Environment int    `json:"environment"`
		HostRead    string `json:"hostRead"`
		HostWrite   string `json:"hostWrite"`
		Spawn       string `json:"spawn"`
		Network     string `json:"network"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid probe output: %v: %s", err, out)
	}
	if result.AsyncResult != 42 || result.Environment != 0 {
		t.Fatalf("async/environment=%+v", result)
	}
	for name, code := range map[string]string{"read": result.HostRead, "write": result.HostWrite, "spawn": result.Spawn, "network": result.Network} {
		if code != "EPERM" && code != "EACCES" {
			t.Errorf("%s was not denied by the OS: %q", name, code)
		}
	}
	t.Logf("installed runtime: %s; NOT CPU/address-space/descriptor/hosted isolation certification", out)
}

func TestDarwinPrototypeCancelsAndReapsLoop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := darwinPrototypeCommand(t, ctx, `console.log("ready");for(;;){}`)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, readErr := bufio.NewReader(stdout).ReadString('\n')
	cancel()
	err = cmd.Wait()
	if readErr != nil || line != "ready\n" {
		t.Fatalf("child did not enter loop: %q %v", line, readErr)
	}
	if err == nil || cmd.ProcessState == nil {
		t.Fatalf("loop was not killed and reaped: state=%v err=%v", cmd.ProcessState, err)
	}
	t.Logf("started JavaScript loop cancelled; Wait reaped pid=%d state=%s", cmd.ProcessState.Pid(), cmd.ProcessState.String())
}
