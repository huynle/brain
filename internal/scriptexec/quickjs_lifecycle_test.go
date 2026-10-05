package scriptexec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The observer is outside the worker and its supervisor. It becomes the Linux
// subreaper, waits for a real framed call (worker is sealed and blocking on IPC),
// kills only the supervisor, then waitpid's the adopted worker itself.
const lifecycleObserver = `
static int transfer(int fd, void *buffer, size_t size, int writing) {
    char *p=buffer;
    while(size) {
        ssize_t n=writing?write(fd,p,size):read(fd,p,size);
        if(n<0 && errno==EINTR)continue;
        if(n<=0)return -1;
        p+=n;size-=n;
    }
    return 0;
}
int main(void) {
    if(prctl(PR_SET_CHILD_SUBREAPER,1,0,0,0))return 70;
    int input[2],output[2];
    if(pipe(input)||pipe(output))return 71;
    pid_t supervisor=fork();
    if(supervisor<0)return 72;
    if(supervisor==0) {
        if(dup2(input[0],0)<0 || dup2(output[1],1)<0)return 73;
        close(input[0]);close(input[1]);close(output[0]);close(output[1]);
        char *args[]={"probe","--supervise",NULL};
        _exit(supervised_main(2,args));
    }
    close(input[0]);close(output[1]);
    char source[]="{\"version\":1,\"kind\":\"call\",\"sequence\":1,\"payload\":\"return await brain.entries.get('blocked');\"}";
    size_t n=strlen(source);
    unsigned char header[4]={n>>24,n>>16,n>>8,n};
    if(transfer(input[1],header,4,1)||transfer(input[1],source,n,1))return 74;
    if(transfer(output[0],header,4,0))return 75;
    n=((unsigned)header[0]<<24)|((unsigned)header[1]<<16)|((unsigned)header[2]<<8)|header[3];
    char call[1024];
    if(!n||n>=sizeof(call)||transfer(output[0],call,n,0))return 76;
    call[n]=0;
    if(!strstr(call,"entries.get"))return 77;
    if(kill(supervisor,SIGKILL))return 78;
    int status=0;
    if(waitpid(supervisor,&status,0)!=supervisor || !WIFSIGNALED(status) || WTERMSIG(status)!=SIGKILL)return 79;
    // Keep the worker's input open. EOF must NOT be what terminates it.
    pid_t child=waitpid(-1,&status,0);
    if(child<=0)return 80;
    int signal=WIFSIGNALED(status)?WTERMSIG(status):0;
    int empty=waitpid(-1,NULL,WNOHANG)==-1 && errno==ECHILD;
    printf("{\"supervisor_killed\":true,\"worker_reaped\":true,\"signal\":%d,\"no_children\":%s}\n",signal,empty?"true":"false");
    close(input[1]);close(output[0]);
    return signal==SIGKILL && empty?0:81;
}
`

func TestQuickJSWorkerSupervisorDeathReapedByObserver(t *testing.T) {
	out := runLifecycleObserver(t, lifecycleObserver)
	var observed struct {
		SupervisorKilled bool `json:"supervisor_killed"`
		WorkerReaped     bool `json:"worker_reaped"`
		Signal           int  `json:"signal"`
		NoChildren       bool `json:"no_children"`
	}
	if err := json.Unmarshal(out, &observed); err != nil {
		t.Fatal(err)
	}
	if !observed.SupervisorKilled || !observed.WorkerReaped || observed.Signal != 9 || !observed.NoChildren {
		t.Fatalf("incomplete death/reaping evidence: %s", out)
	}
	t.Logf("independent native subreaper: %s", out)
}

func TestQuickJSWorkerSupervisorCancellationReapsBeforeExit(t *testing.T) {
	setup, _, ok := strings.Cut(lifecycleObserver, "    if(kill(supervisor,SIGKILL))")
	if !ok {
		t.Fatal("observer anchor changed")
	}
	out := runLifecycleObserver(t, setup+`
    if(kill(supervisor,SIGTERM))return 78;
    int status=0;
    if(waitpid(supervisor,&status,0)!=supervisor)return 79;
    if(!WIFEXITED(status) || WEXITSTATUS(status)!=143)return 80;
    // Cancellation must wait for the worker itself, not orphan it to this observer.
    int empty=waitpid(-1,NULL,WNOHANG)==-1 && errno==ECHILD;
    if(!empty)return 81;
    close(input[1]);close(output[0]);
    return 0;
}
`)
	var status struct {
		Cancelled bool `json:"cancelled"`
		Reaped    bool `json:"reaped"`
		Signal    int  `json:"signal"`
		TimedOut  bool `json:"timed_out"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		t.Fatalf("cancel diagnostic: %v %s", err, out)
	}
	if !status.Cancelled || !status.Reaped || status.Signal != 9 || status.TimedOut {
		t.Fatalf("no cancellation/reap evidence: %s", out)
	}
	t.Logf("cancelled supervisor reaped worker before exit: %s", out)
}

func runLifecycleObserver(t *testing.T, observer string) []byte {
	t.Helper()
	out, err := quickJSProgram(t, "", true, func(host, name string) ([]byte, error) {
		buildCtx, cancelBuild := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancelBuild()
		build := exec.CommandContext(buildCtx, "docker", "--host", host, "exec", "-i", name, "/bin/sh", "-c", `cat > /tmp/observer.c && /bin/sh /tmp/build-probe.sh /tmp observer.c /tmp/observer`)
		worker, e := os.ReadFile("testdata/quickjs_worker.c")
		if e != nil {
			return nil, e
		}
		// Rename only the trusted fixture entry point; worker behavior is unchanged.
		entry := "int main(int argc, char **argv)"
		if strings.Count(string(worker), entry) != 1 {
			return nil, fmt.Errorf("worker entry anchor changed")
		}
		build.Stdin = strings.NewReader(strings.Replace(string(worker), entry, "int supervised_main(int argc, char **argv)", 1) + observer)
		if out, e := build.CombinedOutput(); e != nil {
			return nil, fmt.Errorf("build observer: %w %s", e, out)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", "--host", host, "exec", "--user=65534:65534", name, "/usr/bin/env", "-i", "/tmp/observer")
		cmd.WaitDelay = time.Second
		out, e := cmd.CombinedOutput()
		if ctx.Err() != nil {
			return nil, fmt.Errorf("orphan worker survived supervisor death until outer deadline: %w", ctx.Err())
		}
		if e != nil {
			return nil, fmt.Errorf("observer: %w %s", e, out)
		}
		return out, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
