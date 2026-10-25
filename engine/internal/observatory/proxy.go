package observatory

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"time"
)

// Event deliberately excludes prompts, responses, credentials and request bodies.
type Event struct {
	At               time.Time `json:"at"`
	Model            string    `json:"model"`
	Input            *int64    `json:"input"`
	Output           *int64    `json:"output"`
	EvalNS           *int64    `json:"eval_ns"`
	TotalNS          *int64    `json:"total_ns"`
	RequestedContext int64     `json:"requested_context,omitempty"`
	State            string    `json:"state"`
	DoneReason       string    `json:"done_reason,omitempty"`
	HTTPStatus       int       `json:"http_status"`
}
type finalStats struct {
	Model   string `json:"model"`
	Done    bool   `json:"done"`
	Input   *int64 `json:"prompt_eval_count"`
	Output  *int64 `json:"eval_count"`
	EvalNS  *int64 `json:"eval_duration"`
	TotalNS *int64 `json:"total_duration"`
	Reason  string `json:"done_reason"`
	Error   string `json:"error"`
}

func (s *Server) record(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	if len(s.events) > 10000 {
		s.events = s.events[len(s.events)-10000:]
	}
	if err := atomicJSON(filepath.Join(s.dir, "measurements.json"), s.events); err != nil {
		s.storeError = "Counters are in memory only: " + err.Error()
	} else {
		s.storeError = ""
	}
}
func (s *Server) proxy(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	generation := path == "/api/generate" || path == "/api/chat"
	expected := "GET"
	if generation || path == "/api/show" {
		expected = "POST"
	}
	if r.Method != expected {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body []byte
	var err error
	if r.Method == "POST" {
		body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<20))
		if err != nil {
			http.Error(w, "Request exceeds 16 MiB or could not be read", 413)
			return
		}
	}
	var metadata struct {
		Model   string `json:"model"`
		Options struct {
			NumCtx int64 `json:"num_ctx"`
		} `json:"options"`
	}
	if generation {
		if json.Unmarshal(body, &metadata) != nil || metadata.Model == "" {
			http.Error(w, "A valid JSON model request is required", 400)
			return
		}
	}
	c := s.config()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, r.Method, c.Endpoint+path, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "Invalid endpoint", 500)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	e := Event{At: time.Now(), Model: metadata.Model, RequestedContext: metadata.Options.NumCtx, State: "incomplete"}
	if generation {
		s.mu.Lock()
		s.active[metadata.Model]++
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			s.active[metadata.Model]--
			if s.active[metadata.Model] <= 0 {
				delete(s.active, metadata.Model)
			}
			s.mu.Unlock()
			e.At = time.Now()
			s.record(e)
		}()
	}
	// The context controls long generations; polling uses a separate short-timeout client.
	client := &http.Client{Transport: http.DefaultTransport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		e.State = "connection_error"
		http.Error(w, "Ollama unavailable; check connection settings", 502)
		return
	}
	defer resp.Body.Close()
	e.HTTPStatus = resp.StatusCode
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	if !generation {
		io.Copy(w, resp.Body)
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		e.State = "http_error"
		io.Copy(w, io.LimitReader(resp.Body, 1<<20))
		return
	}
	// Ollama sends NDJSON or one JSON response. Forward bytes as received, retain only
	// a bounded per-line buffer for parsing the terminal statistics. Oversized lines
	// still reach the client, but remain explicitly unmeasured.
	reader := bufio.NewReaderSize(resp.Body, 32<<10)
	line := []byte{}
	oversized := false
	done := false
	for {
		part, readErr := reader.ReadSlice('\n')
		if len(part) > 0 {
			if _, err = w.Write(part); err != nil {
				e.State = "cancelled"
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			if !oversized {
				if len(line)+len(part) <= 4<<20 {
					line = append(line, part...)
				} else {
					oversized = true
					line = nil
				}
			}
		}
		if readErr != bufio.ErrBufferFull {
			if !oversized && len(bytes.TrimSpace(line)) > 0 {
				var st finalStats
				if json.Unmarshal(line, &st) == nil {
					if st.Error != "" {
						e.State = "upstream_error"
					}
					if st.Done && !done {
						done = true
						e.State = "complete"
						e.Input = nonnegative(st.Input)
						e.Output = nonnegative(st.Output)
						e.EvalNS = nonnegative(st.EvalNS)
						e.TotalNS = nonnegative(st.TotalNS)
						e.DoneReason = st.Reason
						if st.Model != "" {
							e.Model = st.Model
						}
					}
				}
			}
			line = nil
			oversized = false
		}
		if readErr != nil && readErr != bufio.ErrBufferFull {
			if readErr != io.EOF && !done {
				e.State = "interrupted"
			}
			break
		}
	}
}
func nonnegative(n *int64) *int64 {
	if n != nil && *n < 0 {
		return nil
	}
	return n
}
