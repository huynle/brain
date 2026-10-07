package scriptexec

import (
	"bytes"
	"os"
	"testing"
)

// Runs the pure mapper as submitted fixture source, not as a registered host
// capability. All unsupported brain methods must remain unsupported afterward.
func TestQuickJSPureFacadeNormalization(t *testing.T) {
	mapper, err := os.ReadFile("testdata/facade-normalization.js")
	if err != nil {
		t.Fatal(err)
	}
	_, err = quickJSProgram(t, "", true, func(host, container string) ([]byte, error) {
		source := "const normalize=" + string(mapper) + `;
const cases=[
 ["health",[],{operation:"health.get",arguments:{}}],
 ["search",[{query:"hello"}],{operation:"search.query",arguments:{request:{query:"hello"}}}],
 ["entries.get",["one",undefined],{operation:"entries.get",arguments:{id:"one"}}],
 ["tasks.resume",["p","t",undefined],{operation:"tasks.resume",arguments:{project:"p",id:"t",request:{}}}],
 ["goals.audit",["g"],{operation:"goals.audit",arguments:{id:"g",limit:50}}],
 ["sections.get",["e","title"],{operation:"sections.get",arguments:{id:"e",title:"title",includeSubsections:false}}],
 ["attachments.detach",["p","e","a","input"],{operation:"attachments.detach",arguments:{project:"p",id:"e",attachmentID:"a",role:"input"}}],
 ["projects.delete",["p","p",true],{operation:"projects.delete",arguments:{project:"p",confirm:"p",force:true}}],
];
for(const [name,args,want] of cases) if(JSON.stringify(normalize(name,args))!==JSON.stringify(want))throw "mapping";
let denied=0,inspected=0;
for(const name of ["entries.iterate","attachments.upload","attachments.download","events.stream","request"]){
 try{normalize(name,new Proxy([],{get(){inspected++;throw "private"}}));}catch(e){if(e.code!=="unsupported_operation")throw "code";denied++;}
}
try{normalize("entries.create",[{get content(){inspected++;throw "private"}}]);}catch(e){if(e.code!=="invalid_arguments")throw "unsafe";denied++;}
let stillDenied=false;try{await brain.tasks.resume("p","t");}catch(e){stillDenied=e.code==="unsupported_operation";}
({mapped:cases.length,denied,inspected,stillDenied});`
		r := bytes.NewReader(runFacadeFixture(t, host, container, source))
		frame, err := ReadFrame(r)
		if err != nil {
			t.Fatal(err)
		}
		parent, _ := NewProtocolSession(ProtocolLimits{100, 65536, 1 << 20})
		m, err := parent.Accept(frame)
		if err != nil || !m.Done || string(m.Result) != `{"mapped":8,"denied":6,"inspected":0,"stillDenied":true}` || r.Len() != 0 {
			t.Fatalf("normalization or zero-IPC boundary: %+v err=%v trailing=%d", m, err, r.Len())
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
