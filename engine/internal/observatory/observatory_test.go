package observatory

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ptr(n int64) *int64 { return &n }
func testServer(t *testing.T, upstream http.HandlerFunc) *Server {
	t.Helper()
	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)
	s, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	s.cfg.Endpoint = up.URL
	return s
}
func TestProxyStreamingPersistenceAndPrivacy(t *testing.T) {
	const wire = "{\"response\":\"PRIVATE_RESPONSE\",\"done\":false}\n{\"model\":\"qwen3:8b\",\"done\":true,\"prompt_eval_count\":200,\"eval_count\":30,\"eval_duration\":2000000000,\"total_duration\":3000000000}\n"
	s := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Error(r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "PRIVATE_PROMPT") {
			t.Error("request not forwarded")
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		io.WriteString(w, wire)
	})
	r := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/chat", strings.NewReader(`{"model":"qwen3:8b","messages":[{"role":"user","content":"PRIVATE_PROMPT"}],"options":{"num_ctx":4096}}`))
	w := httptest.NewRecorder()
	s.Handler("127.0.0.1:8787").ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != wire {
		t.Fatalf("stream altered: %d %s", w.Code, w.Body)
	}
	if len(s.events) != 1 || *s.events[0].Input != 200 || *s.events[0].Output != 30 || s.events[0].State != "complete" || s.events[0].RequestedContext != 4096 {
		t.Fatalf("event: %+v", s.events)
	}
	if len(s.active) != 0 {
		t.Fatal("active count leaked")
	}
	b, e := os.ReadFile(filepath.Join(s.dir, "measurements.json"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), "PRIVATE") {
		t.Fatal("transcript persisted")
	}
	restarted, e := New(s.dir)
	if e != nil || len(restarted.events) != 1 {
		t.Fatalf("restart %v", e)
	}
}
func TestProxyMissingCountersAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body, state string
		status            int
	}{
		{"missing", `{"done":true,"response":"x"}`, "complete", 200},
		{"incomplete", "{\"done\":false}\n", "incomplete", 200},
		{"upstream error", `{"error":"busy"}`, "upstream_error", 200},
		{"quota", `{"error":"rate limited"}`, "http_error", 429},
		{"negative", `{"done":true,"prompt_eval_count":-1,"eval_count":-3}`, "complete", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); io.WriteString(w, tc.body) })
			w := httptest.NewRecorder()
			s.proxy(w, httptest.NewRequest("POST", "/api/generate", strings.NewReader(`{"model":"test","stream":false}`)))
			if w.Code != tc.status || len(s.events) != 1 {
				t.Fatalf("%d %+v", w.Code, s.events)
			}
			e := s.events[0]
			if e.State != tc.state || e.Input != nil || e.Output != nil {
				t.Fatalf("unknown fabricated: %+v", e)
			}
		})
	}
}
func TestProxyFlushesBeforeCompletion(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	s := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "{\"done\":false}\n")
		w.(http.Flusher).Flush()
		close(started)
		<-release
		io.WriteString(w, "{\"done\":true,\"eval_count\":1}\n")
	})
	srv := httptest.NewServer(http.HandlerFunc(s.proxy))
	defer srv.Close()
	resp, e := http.Post(srv.URL+"/api/generate", "application/json", strings.NewReader(`{"model":"test"}`))
	if e != nil {
		close(release)
		t.Fatal(e)
	}
	defer resp.Body.Close()
	<-started
	b := make([]byte, len("{\"done\":false}\n"))
	_, e = io.ReadFull(resp.Body, b)
	close(release)
	if e != nil || !strings.Contains(string(b), "false") {
		t.Fatalf("first chunk: %q %v", b, e)
	}
	io.ReadAll(resp.Body)
}
func TestAccessGuardAndValidation(t *testing.T) {
	s, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	h := s.Handler("127.0.0.1:8787")
	for _, tc := range []struct{ host, origin string }{{"evil.example:8787", ""}, {"127.0.0.1:8787", "https://evil.example"}, {"127.0.0.1:8787", "null"}} {
		r := httptest.NewRequest("GET", "http://"+tc.host+"/obs/config", nil)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("allowed %v", tc)
		}
	}
	for _, endpoint := range []string{"file:///tmp/data", "http://user:pass@host", "http://host/api", "http://host?token=secret", "ftp://host"} {
		c := defaultConfig()
		c.Endpoint = endpoint
		if c.validate() == nil {
			t.Fatalf("accepted %s", endpoint)
		}
	}
	c := defaultConfig()
	c.SSHHost = "-oProxyCommand=bad"
	if c.validate() == nil {
		t.Fatal("SSH option injection")
	}
	c = defaultConfig()
	c.MinTPS = 0
	if c.validate() == nil {
		t.Fatal("invalid threshold")
	}
}
func TestConfigSaveRestart(t *testing.T) {
	s, _ := New(t.TempDir())
	c := defaultConfig()
	c.MinTPS = 25
	b, _ := json.Marshal(c)
	w := httptest.NewRecorder()
	s.Handler("127.0.0.1:8787").ServeHTTP(w, httptest.NewRequest("POST", "http://127.0.0.1:8787/obs/config", strings.NewReader(string(b))))
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	s2, e := New(s.dir)
	if e != nil || s2.cfg.MinTPS != 25 {
		t.Fatalf("config not restored: %v", e)
	}
}

func TestRejectSelfProxyAndMalformedInventory(t *testing.T) {
	s, _ := New(t.TempDir())
	c := defaultConfig()
	c.Endpoint = "http://localhost:8787"
	b, _ := json.Marshal(c)
	w := httptest.NewRecorder()
	s.Handler("127.0.0.1:8787").ServeHTTP(w, httptest.NewRequest("POST", "http://127.0.0.1:8787/obs/config", strings.NewReader(string(b))))
	if w.Code != 400 {
		t.Fatal("self-referencing proxy accepted")
	}
	if !selfEndpoint("http://127.0.0.1:8787", "127.0.0.1:8787") || selfEndpoint("http://127.0.0.1:11434", "127.0.0.1:8787") {
		t.Fatal("wrong self endpoint detection")
	}
	s = testServer(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) })
	status := s.status(context.Background())
	if status.Connected || status.InventoryError == "" {
		t.Fatal("missing arrays treated as measured empty inventory")
	}
}
func TestStatusOfflineAndInventory(t *testing.T) {
	s := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ps" {
			io.WriteString(w, `{"models":[{"name":"qwen3:8b","size":1024,"size_vram":512,"context_length":4096}]}`)
		} else {
			io.WriteString(w, `{"models":[{"name":"qwen3:8b"},{"name":"other"}]}`)
		}
	})
	st := s.status(context.Background())
	if !st.Connected || len(st.Installed) != 2 || len(st.Models) != 1 || st.Host.CPU != nil || st.Host.GPU != nil {
		t.Fatalf("status %+v", st)
	}
	s.cfg.Endpoint = "http://127.0.0.1:1"
	st = s.status(context.Background())
	if st.Connected || st.Error == "" || len(st.Models) != 0 {
		t.Fatal("offline shown as live")
	}
}
func TestHostUsesSecondCPUSampleAndKeepsGPUUnknown(t *testing.T) {
	h := parseHost("17179869184\nCPU usage: 5.0% user, 5.0% sys, 90.0% idle\nPhysMem: 12G used, 4G unused.\nCPU usage: 10.0% user, 20.0% sys, 70.0% idle\nPhysMem: 13G used, 3G unused.\n")
	if h.CPU == nil || *h.CPU != 30 || *h.MemoryTotal != 17179869184 || h.GPU != nil || !strings.Contains(h.MemorySummary, "13G") {
		t.Fatalf("%+v", h)
	}
	h = parseHost("unsupported platform")
	if h.CPU != nil || h.MemoryTotal != nil || h.Error == "" {
		t.Fatal("missing data invented")
	}
}
func TestSuggestionsRequireMeasurementsAndRecentRequests(t *testing.T) {
	cfg := defaultConfig()
	e := Event{At: time.Now(), Model: "test", State: "complete", Input: ptr(3600), Output: ptr(20), EvalNS: ptr(2e9), RequestedContext: 4096}
	st := Status{Connected: true, Events: []Event{e}}
	notes := strings.Join(suggestions(cfg, st), " ")
	if !strings.Contains(notes, "10.0 tok/s") || !strings.Contains(notes, "last prompt") {
		t.Fatal(notes)
	}
	e.At = time.Now().Add(-time.Hour)
	st.Events = []Event{e}
	if len(suggestions(cfg, st)) != 1 || strings.Contains(suggestions(cfg, st)[0], "test:") {
		t.Fatal("stale request used")
	}
	e.At = time.Now()
	e.Input = nil
	e.Output = nil
	st.Events = []Event{e}
	if strings.Contains(strings.Join(suggestions(cfg, st), " "), "test:") {
		t.Fatal("invented measurement")
	}
}
