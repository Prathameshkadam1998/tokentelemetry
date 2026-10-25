package observatory

import (
 "context"
 "fmt"
 "sort"
 "strings"
 "time"

 "github.com/semyonfox/tokentelemetry/engine/internal/cost"
 "github.com/semyonfox/tokentelemetry/engine/internal/ingest"
 "github.com/semyonfox/tokentelemetry/engine/internal/model"
 "github.com/semyonfox/tokentelemetry/engine/internal/pricing"
 "github.com/semyonfox/tokentelemetry/engine/internal/report"
)

// Collection contains accounting metadata only. No turns, transcripts, session IDs,
// project paths, endpoints or credentials cross the SSH collector boundary.
type Collection struct {
 Machine string `json:"machine"`
 ObservedAt time.Time `json:"observed_at"`
 From string `json:"from"`
 To string `json:"to"`
 Rows []ModelUsage `json:"rows"`
 Totals report.Totals `json:"totals"`
 Coverage []Coverage `json:"coverage"`
 Windows []QuotaWindow `json:"windows"`
 Warnings []string `json:"warnings"`
 Duplicates int `json:"duplicates"`
 Excluded int `json:"excluded"`
 PricingDate string `json:"pricing_date"`
 Error string `json:"error,omitempty"`
}
type Coverage struct {
 Agent string `json:"agent"`
 Detected bool `json:"detected"`
 Status string `json:"status"`
 Coverage string `json:"coverage"`
 ExplicitOnly bool `json:"explicit_only"`
 Records int `json:"records"`
 LastRecorded time.Time `json:"last_recorded,omitzero"`
}
type ModelUsage struct {
 Agent string `json:"agent"`
 Model string `json:"model"`
 Provider string `json:"provider"`
 Billing string `json:"billing"`
 Usage model.Usage `json:"usage"`
 ListValue float64 `json:"list_value"`
 Records int `json:"records"`
 Unpriced int `json:"unpriced"`
 AggregateRecords int `json:"aggregate_records"`
 StaleRatedRecords int `json:"stale_rated_records"`
 LastRecorded time.Time `json:"last_recorded,omitzero"`
 LastContext *int64 `json:"last_context"`
}
type QuotaWindow struct {
 model.PlanWindow
 Expired bool `json:"expired"`
 Fresh bool `json:"fresh"`
}

func Collect(ctx context.Context) Collection {
 now:=time.Now();c:=Collection{ObservedAt:now,From:now.AddDate(0,0,-29).Format("2006-01-02"),To:now.Format("2006-01-02")}
 for _,p:=range ingest.Catalog(){c.Coverage=append(c.Coverage,Coverage{Agent:p.Agent,Detected:p.Installed,Status:p.Status,Coverage:p.Coverage,ExplicitOnly:p.ExplicitOnly})}
 // No persistent scan cache on either machine; original logs are always read-only.
 scanners:=ingest.Available();res,err:=ingest.Run(ctx,scanners)
 if err!=nil{c.Error=err.Error();return c}
 tbl,err:=pricing.Load();if err!=nil{c.Error="Pricing could not be loaded: "+err.Error();return c}
 windows,windowErrors:=ingest.PlanWindows(ctx,scanners)
 c=buildCollection(c,res,tbl,windows,now)
 if n:=len(res.Errors)+len(windowErrors);n>0{c.Warnings=append(c.Warnings,fmt.Sprintf("%d source warning(s): some records or plan snapshots could not be read or reconciled. Totals may be incomplete. Run the accounting CLI on this machine for detailed diagnostics; raw diagnostics are withheld here to avoid exporting paths or session identifiers.",n))}
 return c
}

func buildCollection(c Collection,res *ingest.Result,tbl *pricing.Table,windows []model.PlanWindow,now time.Time) Collection {
 rep:=report.Build(res.Turns,tbl,report.Filter{From:c.From,To:c.To},report.Daily,res.Duplicates,nil)
 c.Totals=rep.Totals;c.Duplicates=rep.Duplicates;c.Excluded=len(rep.ExcludedOverlaps);c.PricingDate=fmt.Sprint(rep.PricingDate)
 rows:=map[string]*ModelUsage{};records:=map[string]int{};last:=map[string]time.Time{}
 for _,t:=range res.Turns {
  day:=t.Timestamp.In(time.Local).Format("2006-01-02")
  if t.Timestamp.IsZero()||day<c.From||day>c.To||t.ExcludedReason!=""{continue}
  price:=cost.Of(t,tbl);name:=price.Model;if name==""{name="(unknown model)"}
  provider:=t.Provider;if provider==""{provider="Not recorded"}
  key:=strings.Join([]string{string(t.Agent),name,provider,price.Billing.String()},"\x00")
  row:=rows[key];if row==nil{row=&ModelUsage{Agent:string(t.Agent),Model:name,Provider:provider,Billing:price.Billing.String()};rows[key]=row}
  row.Usage.Add(t.Usage);row.Records++;records[row.Agent]++
  if price.Priced(){row.ListValue+=price.USD}else{row.Unpriced++}
  if price.RateNewerThanCall{row.StaleRatedRecords++};if t.Aggregate{row.AggregateRecords++}
  if t.Timestamp.After(row.LastRecorded){row.LastRecorded=t.Timestamp;row.LastContext=nil;if !t.Aggregate&&t.Usage.ContextTokens>0{n:=t.Usage.ContextTokens;row.LastContext=&n}}
  if t.Timestamp.After(last[row.Agent]){last[row.Agent]=t.Timestamp}
 }
 for _,row:=range rows{c.Rows=append(c.Rows,*row)}
 sort.Slice(c.Rows,func(i,j int)bool{return c.Rows[i].Usage.Total()>c.Rows[j].Usage.Total()})
 for i:=range c.Coverage{c.Coverage[i].Records=records[c.Coverage[i].Agent];c.Coverage[i].LastRecorded=last[c.Coverage[i].Agent]}
 for _,w:=range windows{expired:=ingest.PlanWindowExpired(w,now);age:=now.Sub(w.ObservedAt);c.Windows=append(c.Windows,QuotaWindow{PlanWindow:w,Expired:expired,Fresh:!expired&&!w.ObservedAt.IsZero()&&age>=0&&age<=15*time.Minute})}
 return c
}

