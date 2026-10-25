package observatory

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var assets embed.FS

type Config struct {
	Endpoint       string  `json:"endpoint"`
	SSHHost        string  `json:"ssh_host"`
	Tunnel         bool    `json:"tunnel"`
	MinTPS         float64 `json:"min_tps"`
	ContextWarning float64 `json:"context_warning"`
	CPUWarning     float64 `json:"cpu_warning"`
	QuotaWarning   float64 `json:"quota_warning"`
	RemoteHistory  bool    `json:"remote_history"`
}

func defaultConfig() Config {
	return Config{Endpoint: "http://127.0.0.1:11434", MinTPS: 15, ContextWarning: 80, CPUWarning: 85, QuotaWarning:90}
}

var sshPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@:-]*$`)

func (c Config) validate() error {
	u, e := url.Parse(c.Endpoint)
	if e != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("endpoint must be an http(s) origin without credentials, path, query or fragment")
	}
	if c.SSHHost != "" && !sshPattern.MatchString(c.SSHHost) {
		return errors.New("SSH destination must be a host or user@host; configure ports/keys in your SSH config")
	}
	if c.Tunnel && (c.SSHHost == "" || c.Endpoint != "http://127.0.0.1:11435") {
		return errors.New("SSH tunnel requires a destination and endpoint http://127.0.0.1:11435")
	}
	if c.MinTPS <= 0 || c.MinTPS > 10000 || c.ContextWarning < 1 || c.ContextWarning > 100 || c.CPUWarning < 1 || c.CPUWarning > 100 {
		return errors.New("thresholds must be positive; percentage thresholds must be 1–100")
	}
	if c.QuotaWarning<1||c.QuotaWarning>100{return errors.New("quota threshold must be 1–100 percent")}
	return nil
}

type Server struct {
	mu               sync.Mutex
	cfg              Config
	dir              string
	events           []Event
	active           map[string]int
	storeError       string
	tunnelMu         sync.Mutex
	tunnelCancel     context.CancelFunc
	tunnelDone       chan struct{}
	tunnelStarted    time.Time
	tunnelError      string
	tunnelGeneration int
	client           *http.Client
	hostMu           sync.Mutex
	hostCache        HostMetrics
	hostAt           time.Time
	historyMu        sync.Mutex
	historyCache     History
	historyAt        time.Time
	fleetMu          sync.Mutex
	fleetAt          time.Time
	fleetCache       Fleet
	remoteInstallMu  sync.Mutex
}

func New(dir string) (*Server, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &Server{dir: dir, cfg: defaultConfig(), active: map[string]int{}, client: &http.Client{Timeout: 8 * time.Second}}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err == nil {
		if err = json.Unmarshal(data, &s.cfg); err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err = s.cfg.validate(); err != nil {
		return nil, err
	}
	data, err = os.ReadFile(filepath.Join(dir, "measurements.json"))
	if err == nil {
		if err = json.Unmarshal(data, &s.events); err != nil {
			return nil, fmt.Errorf("read measurements: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if len(s.events) > 10000 {
		s.events = s.events[len(s.events)-10000:]
	}
	return s, nil
}
func atomicJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".write-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	return os.Rename(name, path)
}
func (s *Server) config() Config { s.mu.Lock(); defer s.mu.Unlock(); return s.cfg }
func (s *Server) startTunnel() {
	s.tunnelMu.Lock()
	defer s.tunnelMu.Unlock()
	s.stopTunnel()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tunnelGeneration++
	generation := s.tunnelGeneration
	s.tunnelError = ""
	if !s.cfg.Tunnel {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.tunnelCancel = cancel
	done := make(chan struct{})
	s.tunnelDone = done
	s.tunnelStarted = time.Now()
	host := s.cfg.SSHHost
	go func() {
		defer close(done)
		// No key acceptance, password prompts, shell interpolation, or remote changes.
		cmd := exec.CommandContext(ctx, "ssh", "-N", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=5", "-o", "ExitOnForwardFailure=yes", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", "-L", "127.0.0.1:11435:127.0.0.1:11434", host)
		hideWindow(cmd)
		out, err := cmd.CombinedOutput()
		if err != nil && ctx.Err() == nil {
			s.mu.Lock()
			if s.tunnelGeneration == generation {
				s.tunnelError = fmt.Sprintf("SSH tunnel stopped: %s (%v). Check SSH access and port 11435, then reconnect.", strings.TrimSpace(string(out)), err)
			}
			s.mu.Unlock()
		}
	}()
}
func (s *Server) Close() {
	s.tunnelMu.Lock()
	defer s.tunnelMu.Unlock()
	s.stopTunnel()
}

// Caller owns tunnelMu. Wait outside mu so an exiting process can report its status.
func (s *Server) stopTunnel() {
	s.mu.Lock()
	cancel, done := s.tunnelCancel, s.tunnelDone
	s.tunnelCancel, s.tunnelDone = nil, nil
	s.tunnelGeneration++
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}
func Run(dir string, port int) error {
	if port < 1 || port > 65535 {
		return errors.New("invalid port")
	}
	s, err := New(dir)
	if err != nil {
		return err
	}
	defer s.Close()
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if selfEndpoint(s.config().Endpoint, addr) {
		return errors.New("Ollama endpoint points back to this observatory; use the upstream Ollama port")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.startTunnel()
	server := &http.Server{Handler: s.Handler(addr), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	log.Printf("TokenTelemetry Observatory: http://%s (data: %s)", addr, dir)
	err = server.Serve(ln)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
func (s *Server) Handler(addr string) http.Handler {
	mux := http.NewServeMux()
	s.fleetRoutes(mux)
	mux.HandleFunc("/obs/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "method not allowed", 405)
			return
		}
		writeJSON(w, map[string]string{"application": "TokenTelemetryObservatory"})
	})
	mux.HandleFunc("/obs/disconnect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", 405)
			return
		}
		s.Close()
		writeJSON(w, map[string]bool{"disconnected": true})
	})
	web, _ := fs.Sub(assets, "web")
	mux.Handle("/", http.FileServer(http.FS(web)))
	mux.HandleFunc("/obs/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			writeJSON(w, s.config())
			return
		}
		if r.Method != "POST" {
			http.Error(w, "method not allowed", 405)
			return
		}
		c := defaultConfig()
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		d.DisallowUnknownFields()
		if e := d.Decode(&c); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		c.Endpoint = strings.TrimRight(strings.TrimSpace(c.Endpoint), "/")
		c.SSHHost = strings.TrimSpace(c.SSHHost)
		if e := c.validate(); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if selfEndpoint(c.Endpoint, addr) {
			http.Error(w, "Ollama endpoint points back to this observatory. Use the upstream Ollama port; configure your client with the proxy URL instead.", 400)
			return
		}
		s.mu.Lock()
		e := atomicJSON(filepath.Join(s.dir, "config.json"), c)
		if e == nil {
			s.cfg = c
		}
		s.mu.Unlock()
		if e != nil {
			http.Error(w, "Could not save configuration", 500)
			return
		}
		s.hostMu.Lock()
		s.hostAt = time.Time{}
		s.hostMu.Unlock()
		s.startTunnel()
		s.fleetMu.Lock();s.fleetAt=time.Time{};s.fleetMu.Unlock()
		writeJSON(w, c)
	})
	mux.HandleFunc("/obs/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "method not allowed", 405)
			return
		}
		writeJSON(w, s.status(r.Context()))
	})
	mux.HandleFunc("/obs/history", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "method not allowed", 405)
			return
		}
		writeJSON(w, s.history(r.Context()))
	})
	for _, path := range []string{"/api/generate", "/api/chat", "/api/tags", "/api/ps", "/api/show", "/api/version"} {
		mux.HandleFunc(path, s.proxy)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Loopback binding plus Host and Origin checks prevent browser cross-origin access and DNS rebinding.
		if r.Host != addr {
			http.Error(w, "Use http://"+addr, 403)
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && origin != "http://"+addr {
			http.Error(w, "foreign origin rejected", 403)
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "cross-site request rejected", 403)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		mux.ServeHTTP(w, r)
	})
}

func selfEndpoint(endpoint, addr string) bool {
	u, e := url.Parse(endpoint)
	if e != nil {
		return false
	}
	_, port, e := net.SplitHostPort(addr)
	if e != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	ip := net.ParseIP(host)
	return u.Port() == port && (host == "localhost" || (ip != nil && ip.IsLoopback()))
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
func (s *Server) get(ctx context.Context, c Config, path string, v any) error {
	req, e := http.NewRequestWithContext(ctx, "GET", c.Endpoint+path, nil)
	if e != nil {
		return e
	}
	resp, e := s.client.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Ollama returned HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v)
}

type History struct {
	Report     json.RawMessage `json:"report,omitempty"`
	Error      string          `json:"error,omitempty"`
	Warnings   string          `json:"warnings,omitempty"`
	ObservedAt time.Time       `json:"observed_at"`
}

func (s *Server) history(ctx context.Context) History {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	if time.Since(s.historyAt) < 60*time.Second {
		return s.historyCache
	}
	h := History{ObservedAt: time.Now()}
	exe, e := os.Executable()
	if e != nil {
		h.Error = e.Error()
		return h
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "accounting", "summary", "--json")
	hideWindow(cmd)
	// Offline pricing and source logs stay on the laptop. Upstream's scan cache is separate.
	cmd.Env = append(os.Environ(), "TT_OFFLINE=1")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if e = cmd.Run(); e != nil {
		h.Error = strings.TrimSpace(stderr.String())
		if h.Error == "" {
			h.Error = e.Error()
		}
	} else if json.Valid(out.Bytes()) {
		h.Report = append([]byte(nil), out.Bytes()...)
		h.Warnings = strings.TrimSpace(stderr.String())
	} else {
		h.Error = "Accounting engine returned invalid JSON"
	}
	s.historyCache = h
	s.historyAt = time.Now()
	return h
}
