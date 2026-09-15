package sessionmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"skills-browserauth/site"
	"skills-browserauth/store"
)

type Request struct {
	Action  string            `json:"action"`
	Site    string            `json:"site,omitempty"`
	Method  string            `json:"method,omitempty"`
	Path    string            `json:"path,omitempty"`
	Body    string            `json:"body,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type SessionInfo struct {
	Site         string    `json:"site"`
	CreatedAt    time.Time `json:"created_at"`
	LastUsed     time.Time `json:"last_used"`
	RequestCount int64     `json:"request_count"`
}

type Response struct {
	OK       bool          `json:"ok"`
	Error    string        `json:"error,omitempty"`
	Status   int           `json:"status,omitempty"`
	Body     string        `json:"body,omitempty"`
	Username string        `json:"username,omitempty"`
	Sessions []SessionInfo `json:"sessions,omitempty"`
}

type managedSession struct {
	cfg     site.Config
	browser *site.BrowserSession
}

type Server struct {
	mu       sync.Mutex
	sessions map[string]*managedSession
	stop     chan struct{}
	once     sync.Once
}

func SocketPath() string { return filepath.Join(store.DefaultDataDir(), "run", "session.sock") }

func NewServer() *Server {
	return &Server{sessions: make(map[string]*managedSession), stop: make(chan struct{})}
}

func (s *Server) get(siteID string) (*managedSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current := s.sessions[siteID]; current != nil {
		return current, nil
	}
	cfg, err := site.Load(siteID)
	if err != nil {
		return nil, err
	}
	if cfg.Auth.Transport != "browser" {
		return nil, fmt.Errorf("site %q does not use auth.transport: browser", siteID)
	}
	cookie, err := store.ResolveCookie(cfg.StoreNames())
	if err != nil {
		return nil, err
	}
	browser, err := cfg.OpenBrowserSession(context.Background(), cookie, true)
	if err != nil {
		return nil, err
	}
	managed := &managedSession{cfg: cfg, browser: browser}
	s.sessions[siteID] = managed
	return managed, nil
}

func (s *Server) closeSite(siteID string) bool {
	s.mu.Lock()
	managed := s.sessions[siteID]
	delete(s.sessions, siteID)
	s.mu.Unlock()
	if managed != nil {
		managed.browser.Close()
		return true
	}
	return false
}

func (s *Server) closeAll() {
	s.mu.Lock()
	all := s.sessions
	s.sessions = make(map[string]*managedSession)
	s.mu.Unlock()
	for _, managed := range all {
		managed.browser.Close()
	}
}

func (s *Server) snapshot() []SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]SessionInfo, 0, len(s.sessions))
	for id, managed := range s.sessions {
		created, used, count := managed.browser.Stats()
		result = append(result, SessionInfo{Site: id, CreatedAt: created, LastUsed: used, RequestCount: count})
	}
	return result
}

func (s *Server) Handle(req Request) Response {
	switch req.Action {
	case "ping", "status":
		return Response{OK: true, Sessions: s.snapshot()}
	case "start", "auth":
		managed, err := s.get(req.Site)
		if err != nil {
			return Response{Error: err.Error()}
		}
		session, err := managed.browser.Authenticate()
		if err != nil {
			s.closeSite(req.Site)
			return Response{Error: err.Error()}
		}
		return Response{OK: true, Username: session.Username}
	case "request":
		managed, err := s.get(req.Site)
		if err != nil {
			return Response{Error: err.Error()}
		}
		body, status, err := managed.browser.Request(req.Method, req.Path, req.Body, req.Headers)
		if err != nil {
			return Response{Error: err.Error(), Status: status, Body: string(body)}
		}
		return Response{OK: true, Status: status, Body: string(body)}
	case "stop_session":
		return Response{OK: true, Body: fmt.Sprintf("%t", s.closeSite(req.Site))}
	case "shutdown":
		s.once.Do(func() { close(s.stop) })
		return Response{OK: true}
	default:
		return Response{Error: fmt.Sprintf("unknown session manager action %q", req.Action)}
	}
}

func (s *Server) reapExpired() {
	now := time.Now()
	s.mu.Lock()
	var expired []string
	for id, managed := range s.sessions {
		created, used, _ := managed.browser.Stats()
		idleTimeout := managed.cfg.BrowserIdleTimeout()
		maxLifetime := managed.cfg.BrowserMaxLifetime()
		if !managed.browser.Alive() || (idleTimeout > 0 && now.Sub(used) >= idleTimeout) || (maxLifetime > 0 && now.Sub(created) >= maxLifetime) {
			expired = append(expired, id)
		}
	}
	s.mu.Unlock()
	for _, id := range expired {
		s.closeSite(id)
	}
}

func (s *Server) Serve() error {
	path := SocketPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if conn, err := net.DialTimeout("unix", path, 200*time.Millisecond); err == nil {
		conn.Close()
		return fmt.Errorf("browser session manager is already running")
	}
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(path)
	defer s.closeAll()
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}

	go func() {
		<-s.stop
		listener.Close()
	}()
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.reapExpired()
			case <-s.stop:
				return
			}
		}
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-s.stop:
				return nil
			default:
				return err
			}
		}
		go func() {
			defer conn.Close()
			var req Request
			if err := json.NewDecoder(conn).Decode(&req); err != nil {
				_ = json.NewEncoder(conn).Encode(Response{Error: err.Error()})
				return
			}
			_ = json.NewEncoder(conn).Encode(s.Handle(req))
		}()
	}
}

func Call(req Request) (Response, error) {
	conn, err := net.DialTimeout("unix", SocketPath(), time.Second)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(90 * time.Second))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}
	var response Response
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return Response{}, err
	}
	return response, nil
}

func EnsureStarted() error {
	if response, err := Call(Request{Action: "ping"}); err == nil && response.OK {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	runDir := filepath.Dir(SocketPath())
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(runDir, "session.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	command := exec.Command(executable, "_session_manager", "serve")
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	command.Stdin = nil
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		return err
	}
	_ = command.Process.Release()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if response, err := Call(Request{Action: "ping"}); err == nil && response.OK {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("browser session manager did not start; see ~/.browserauth/run/session.log")
}
