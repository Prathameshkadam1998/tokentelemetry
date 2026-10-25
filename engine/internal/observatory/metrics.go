package observatory

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type OllamaModel struct {
	Name          string `json:"name"`
	Size          int64  `json:"size"`
	SizeVRAM      *int64 `json:"size_vram,omitempty"`
	ContextLength *int64 `json:"context_length,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	Details       struct {
		ParameterSize string `json:"parameter_size"`
		Quantization  string `json:"quantization_level"`
	} `json:"details"`
}
type HostMetrics struct {
	ObservedAt    time.Time `json:"observed_at"`
	CPU           *float64  `json:"cpu_percent"`
	MemoryTotal   *int64    `json:"memory_total"`
	MemorySummary string    `json:"memory_summary"`
	GPU           *float64  `json:"gpu_percent"`
	Error         string    `json:"error,omitempty"`
	Source        string    `json:"source"`
}

var cpuRE = regexp.MustCompile(`CPU usage:.*?([\d.]+)% idle`)
var memoryRE = regexp.MustCompile(`(?m)^PhysMem: (.+)$`)

func parseHost(out string) HostMetrics {
	h := HostMetrics{ObservedAt: time.Now(), Source: "macOS top / sysctl over SSH; whole host"}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > 0 {
		if n, e := strconv.ParseInt(strings.TrimSpace(lines[0]), 10, 64); e == nil && n > 0 {
			h.MemoryTotal = &n
		}
	}
	matches := cpuRE.FindAllStringSubmatch(out, -1)
	if len(matches) > 0 {
		idle, e := strconv.ParseFloat(matches[len(matches)-1][1], 64)
		if e == nil && idle >= 0 && idle <= 100 {
			v := 100 - idle
			h.CPU = &v
		}
	}
	matches = memoryRE.FindAllStringSubmatch(out, -1)
	if len(matches) > 0 {
		h.MemorySummary = matches[len(matches)-1][1]
	}
	if h.CPU == nil || h.MemoryTotal == nil {
		h.Error = "macOS CPU or memory output could not be parsed; unsupported fields remain unavailable"
	}
	return h
}
func (s *Server) host(ctx context.Context, c Config) HostMetrics {
	s.hostMu.Lock()
	defer s.hostMu.Unlock()
	if time.Since(s.hostAt) < 15*time.Second {
		return s.hostCache
	}
	if c.SSHHost == "" {
		return HostMetrics{Error: "Configure SSH to read Mac CPU and memory. GPU utilization is unavailable in this version."}
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=4", c.SSHHost, "LC_ALL=C; export LC_ALL; sysctl -n hw.memsize; top -l 2 -s 1 -n 0")
	hideWindow(cmd)
	out, e := cmd.Output()
	h := parseHost(string(out))
	if e != nil {
		h = HostMetrics{ObservedAt: time.Now(), Error: "Mac telemetry unavailable: " + e.Error() + ". Verify non-interactive SSH access."}
	}
	s.hostCache = h
	s.hostAt = time.Now()
	return h
}

type Status struct {
	ObservedAt     time.Time      `json:"observed_at"`
	Connected      bool           `json:"connected"`
	Error          string         `json:"error,omitempty"`
	InventoryError string         `json:"inventory_error,omitempty"`
	Models         []OllamaModel  `json:"models"`
	Installed      []OllamaModel  `json:"installed"`
	Host           HostMetrics    `json:"host"`
	Events         []Event        `json:"events"`
	Active         map[string]int `json:"active"`
	Suggestions    []string       `json:"suggestions"`
	StoreError     string         `json:"store_error,omitempty"`
	TunnelError    string         `json:"tunnel_error,omitempty"`
}

func (s *Server) status(ctx context.Context) Status {
	c := s.config()
	v := Status{ObservedAt: time.Now()}
	var ps, tags struct {
		Models []OllamaModel `json:"models"`
	}
	var pe, te error
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); pe = s.get(ctx, c, "/api/ps", &ps) }()
	go func() { defer wg.Done(); te = s.get(ctx, c, "/api/tags", &tags) }()
	go func() { defer wg.Done(); v.Host = s.host(ctx, c) }()
	wg.Wait()
	// The first HTTP poll can beat the SSH port forward. Host sampling above
	// gives the tunnel time to establish; retry once during its startup window.
	s.mu.Lock()
	starting := c.Tunnel && time.Since(s.tunnelStarted) < 10*time.Second && s.tunnelError == ""
	s.mu.Unlock()
	if starting && pe != nil {
		pe = s.get(ctx, c, "/api/ps", &ps)
	}
	if starting && te != nil {
		te = s.get(ctx, c, "/api/tags", &tags)
	}
	if pe == nil && ps.Models == nil {
		pe = fmt.Errorf("Ollama /api/ps did not return a models array")
	}
	if te == nil && tags.Models == nil {
		te = fmt.Errorf("Ollama /api/tags did not return a models array")
	}
	v.Connected = pe == nil
	v.Models = ps.Models
	v.Installed = tags.Models
	if pe != nil {
		v.Error = pe.Error()
	}
	if te != nil {
		v.InventoryError = te.Error()
	}
	s.mu.Lock()
	v.Events = append([]Event{}, s.events...)
	v.Active = map[string]int{}
	for k, n := range s.active {
		v.Active[k] = n
	}
	v.StoreError = s.storeError
	v.TunnelError = s.tunnelError
	s.mu.Unlock()
	v.Suggestions = suggestions(c, v)
	return v
}
func suggestions(c Config, v Status) []string {
	notes := []string{}
	if !v.Connected {
		notes = append(notes, "Connect Ollama before interpreting model availability. No usage is inferred while the connection is unavailable.")
	}
	// Compare only the latest completed, measured request per model within 15 minutes.
	seen := map[string]bool{}
	for i := len(v.Events) - 1; i >= 0; i-- {
		e := v.Events[i]
		if seen[e.Model] || e.State != "complete" || time.Since(e.At) > 15*time.Minute {
			continue
		}
		seen[e.Model] = true
		if e.Output != nil && e.EvalNS != nil && *e.Output > 0 && *e.EvalNS > 0 {
			tps := float64(*e.Output) * 1e9 / float64(*e.EvalNS)
			if tps < c.MinTPS {
				notes = append(notes, fmt.Sprintf("%s: last measured decode speed %.1f tok/s is below your %.1f target. Compare a smaller or more quantized model on the same task; check concurrent work first. Speed alone does not establish quality.", e.Model, tps, c.MinTPS))
			}
		}
		if e.Input != nil && e.RequestedContext > 0 && float64(*e.Input)/float64(e.RequestedContext)*100 >= c.ContextWarning {
			notes = append(notes, fmt.Sprintf("%s: last prompt used %d tokens against a requested %d-token window (threshold %.0f%%). Shorten or summarize the conversation, or evaluate a larger supported context window. This is a last-request comparison, not live occupancy; the server may override the requested window.", e.Model, *e.Input, e.RequestedContext, c.ContextWarning))
		}
		if e.DoneReason == "length" {
			notes = append(notes, e.Model+": the last response stopped at a length limit. Check the output limit and context settings; this does not mean Ollama has a cumulative token quota.")
		}
	}
	if v.Host.CPU != nil && *v.Host.CPU >= c.CPUWarning {
		notes = append(notes, fmt.Sprintf("Mac CPU is %.1f%% at this sample, above your %.0f%% threshold. Check other processes and repeat measurements before changing models.", *v.Host.CPU, c.CPUWarning))
	}
	if len(notes) == 0 {
		if len(seen) == 0 {
			notes = append(notes, "No recent measured requests yet. Route your Ollama client through the laptop proxy to collect speed and token evidence. Model suitability also depends on task quality.")
		} else {
			notes = append(notes, "No threshold crossed in recent measured requests. Model suitability still depends on task quality; use a representative task to compare candidates.")
		}
	}
	return notes
}
