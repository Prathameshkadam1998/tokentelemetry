package observatory

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "sync"
 "time"
)

const remoteCollector=`"$HOME/.local/share/tokentelemetry-observatory/collector"`
type Fleet struct {Nodes []Collection `json:"nodes"`;ObservedAt time.Time `json:"observed_at"`;Suggestions []string `json:"suggestions"`}

// A bounded output buffer prevents oversized or unexpected subprocess output
// from exhausting the dashboard's memory. The collector returns aggregates only.
type cappedBuffer struct { bytes.Buffer; max int }
func(b *cappedBuffer)Write(p []byte)(int,error){if b.Len()+len(p)>b.max{return 0,errors.New("collector output exceeds limit")};return b.Buffer.Write(p)}
func decodeCollection(cmd *exec.Cmd,machine string) Collection {
 out:=&cappedBuffer{max:16<<20};errOut:=&cappedBuffer{max:64<<10};cmd.Stdout=out;cmd.Stderr=errOut;hideWindow(cmd)
 err:=cmd.Run();var c Collection
 if err!=nil {return Collection{Machine:machine,ObservedAt:time.Now(),Error:"Collection unavailable. Verify SSH/collector setup or run the local accounting CLI. Collector timed out or exited unsuccessfully."}}
 if json.Unmarshal(out.Bytes(),&c)!=nil||c.ObservedAt.IsZero()||len(c.Coverage)==0{return Collection{Machine:machine,ObservedAt:time.Now(),Error:"Collector returned an unsupported or incomplete snapshot; reinstall the matching collector."}}
 c.Machine=machine;return c
}
func (s *Server) fleet(ctx context.Context,force bool) Fleet {
 s.fleetMu.Lock();defer s.fleetMu.Unlock()
 if !force&&time.Since(s.fleetAt)<60*time.Second{return s.fleetCache}
 c:=s.config();result:=Fleet{ObservedAt:time.Now(),Nodes:make([]Collection,2)}
 var wg sync.WaitGroup;wg.Add(2)
 go func(){defer wg.Done();exe,e:=os.Executable();if e!=nil{result.Nodes[0]=Collection{Machine:"Laptop",Error:e.Error()};return};ctx,cancel:=context.WithTimeout(ctx,95*time.Second);defer cancel();cmd:=exec.CommandContext(ctx,exe,"collect");cmd.Env=append(os.Environ(),"TT_OFFLINE=1");result.Nodes[0]=decodeCollection(cmd,"Laptop")}()
 go func(){defer wg.Done();if !c.RemoteHistory||c.SSHHost==""{result.Nodes[1]=Collection{Machine:"Mac",Error:"Mac agent history is disabled. Install the on-demand collector and enable Mac history in setup."};return};ctx,cancel:=context.WithTimeout(ctx,100*time.Second);defer cancel();cmd:=exec.CommandContext(ctx,"ssh","-o","BatchMode=yes","-o","StrictHostKeyChecking=yes","-o","ConnectTimeout=5",c.SSHHost,"TT_OFFLINE=1 "+remoteCollector);result.Nodes[1]=decodeCollection(cmd,"Mac")}()
 wg.Wait();result.Suggestions=fleetSuggestions(result,c,time.Now());s.fleetCache=result;s.fleetAt=time.Now();return result
}
func fleetSuggestions(f Fleet,c Config,now time.Time)[]string{
 notes:=[]string{}
 for _,n:=range f.Nodes{if n.Error!=""{continue};for _,q:=range n.Windows{
  age:=now.Sub(q.ObservedAt)
  if q.Fresh&&!q.Expired&&age>=0&&age<=15*time.Minute&&q.UsedPercent>=c.QuotaWarning{
   action:="is nearing its reported limit";if q.UsedPercent>=100{action="reported an exhausted window"}
   notes=append(notes,fmt.Sprintf("%s / %s %s %s (%.1f%% used; observed %s). Check the provider's current limit before starting long work. Wait for reset or use an independently provisioned route; switching models within the same plan may share this limit.",n.Machine,q.Agent,q.Window,action,q.UsedPercent,q.ObservedAt.Format(time.RFC3339)))
  }
 }}
 if len(notes)==0{notes=append(notes,"No fresh source-reported quota crosses your warning threshold. Missing or old snapshots do not establish available quota. Compare model quality on your task; historical tokens and host CPU cannot determine it.")}
 return notes
}
func (s *Server) installCollector(ctx context.Context) error {
 s.remoteInstallMu.Lock();defer s.remoteInstallMu.Unlock();c:=s.config();if c.SSHHost==""{return errors.New("save an SSH destination first")}
 ctx,cancel:=context.WithTimeout(ctx,90*time.Second);defer cancel()
 cmd:=exec.CommandContext(ctx,"ssh","-o","BatchMode=yes","-o","StrictHostKeyChecking=yes","-o","ConnectTimeout=5",c.SSHHost,"uname -sm");hideWindow(cmd);out,e:=cmd.Output();if e!=nil{return errors.New("SSH platform check failed; verify existing non-interactive access")}
 arch:="";switch strings.TrimSpace(string(out)){case "Darwin arm64":arch="arm64";case "Darwin x86_64":arch="amd64";default:return errors.New("on-demand collector supports macOS ARM64 or Intel only")}
 exe,e:=os.Executable();if e!=nil{return e};file,e:=os.Open(filepath.Join(filepath.Dir(exe),"collector-darwin-"+arch));if e!=nil{return errors.New("collector binary missing from installed bundle; reinstall the complete package")};defer file.Close()
 // Fixed destination in our own directory; never run arbitrary user-provided shell text.
 script:=`set -eu; umask 077; mkdir -p "$HOME/.local/share/tokentelemetry-observatory"; cat > "$HOME/.local/share/tokentelemetry-observatory/collector.upload"; chmod 700 "$HOME/.local/share/tokentelemetry-observatory/collector.upload"; mv "$HOME/.local/share/tokentelemetry-observatory/collector.upload" "$HOME/.local/share/tokentelemetry-observatory/collector"`
 cmd=exec.CommandContext(ctx,"ssh","-o","BatchMode=yes","-o","StrictHostKeyChecking=yes","-o","ConnectTimeout=5",c.SSHHost,script);cmd.Stdin=file;cmd.Stdout=io.Discard;hideWindow(cmd)
 if e=cmd.Run();e!=nil{return errors.New("collector transfer failed; existing collector was not activated unless transfer completed")}
 return nil
}
func (s *Server) fleetRoutes(mux *http.ServeMux){
 mux.HandleFunc("/obs/fleet",func(w http.ResponseWriter,r *http.Request){if r.Method!="GET"{http.Error(w,"method not allowed",405);return};writeJSON(w,s.fleet(r.Context(),false))})
 mux.HandleFunc("/obs/fleet/refresh",func(w http.ResponseWriter,r *http.Request){if r.Method!="POST"{http.Error(w,"method not allowed",405);return};writeJSON(w,s.fleet(r.Context(),true))})
 mux.HandleFunc("/obs/collector/install",func(w http.ResponseWriter,r *http.Request){if r.Method!="POST"{http.Error(w,"method not allowed",405);return};if e:=s.installCollector(r.Context());e!=nil{http.Error(w,e.Error(),500);return};writeJSON(w,map[string]string{"status":"Collector installed. Enable Mac history and save to read aggregates."})})
}
