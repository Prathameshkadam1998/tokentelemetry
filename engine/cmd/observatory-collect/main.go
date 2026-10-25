// One-shot, read-only collector. It starts no listener and returns aggregates only.
package main
import("context";"encoding/json";"os";"time";"github.com/semyonfox/tokentelemetry/engine/internal/observatory")
func main(){os.Setenv("TT_OFFLINE","1");ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second);defer cancel();c:=observatory.Collect(ctx);json.NewEncoder(os.Stdout).Encode(c);if c.Error!=""{os.Exit(1)}}
