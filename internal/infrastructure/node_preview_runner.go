package infrastructure

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Liapoldus/Constructor/internal/domain"
)

const previewStartupTimeout = 30 * time.Second

type NodePreviewRunner struct {
	mu     sync.Mutex
	active *nodePreviewProcess
}

type nodePreviewProcess struct {
	root        string
	url         string
	upstreamURL string
	sessionID   string
	cmd         *exec.Cmd
	cancel      context.CancelFunc
	done        chan struct{}
	proxy       *http.Server
	listener    net.Listener
	err         error
}

func NewNodePreviewRunner() *NodePreviewRunner { return &NodePreviewRunner{} }

func (r *NodePreviewRunner) Start(root string) (domain.PreviewSession, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return domain.PreviewSession{}, err
	}
	if _, err := os.Stat(filepath.Join(absRoot, "liapoldus", "project.json")); err != nil {
		return domain.PreviewSession{}, fmt.Errorf("preview requires liapoldus/project.json: %w", err)
	}
	var packageJSON struct {
		Scripts map[string]string `json:"scripts"`
	}
	manifest, err := os.ReadFile(filepath.Join(absRoot, "package.json"))
	if err != nil {
		return domain.PreviewSession{}, fmt.Errorf("preview requires package.json: %w", err)
	}
	if err := json.Unmarshal(manifest, &packageJSON); err != nil {
		return domain.PreviewSession{}, fmt.Errorf("read preview package.json: %w", err)
	}
	if strings.TrimSpace(packageJSON.Scripts["dev"]) == "" {
		return domain.PreviewSession{}, fmt.Errorf("project package.json must define scripts.dev")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active != nil && r.active.root == absRoot && r.isRunning(r.active) {
		return previewSession(r.active), nil
	}
	if err := r.stopLocked(); err != nil {
		return domain.PreviewSession{}, err
	}
	if err := installNodeDependencies(absRoot); err != nil {
		return domain.PreviewSession{}, err
	}
	upstreamPort, err := freeLoopbackPort()
	if err != nil {
		return domain.PreviewSession{}, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return domain.PreviewSession{}, fmt.Errorf("allocate preview proxy: %w", err)
	}
	upstreamURL := fmt.Sprintf("http://127.0.0.1:%d/", upstreamPort)
	sessionBytes := make([]byte, 16)
	if _, err := rand.Read(sessionBytes); err != nil {
		return domain.PreviewSession{}, fmt.Errorf("create preview session id: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "npm", "run", "dev", "--", "--host", "127.0.0.1", "--port", strconv.Itoa(upstreamPort), "--strictPort")
	configureProcessTree(cmd)
	cmd.Dir = absRoot
	cmd.Env = projectProcessEnvironment(os.Environ())
	cmd.Stdout = os.Stderr // Keep startup and runtime diagnostics visible to the local operator.
	cmd.Stderr = os.Stderr
	sessionID := hex.EncodeToString(sessionBytes)
	proxyHost := fmt.Sprintf("%s.localhost:%d", sessionID, listener.Addr().(*net.TCPAddr).Port)
	upstream, _ := url.Parse(upstreamURL)
	process := &nodePreviewProcess{
		root: absRoot, url: "http://" + proxyHost + "/", upstreamURL: upstreamURL,
		sessionID: sessionID, cmd: cmd, cancel: cancel, done: make(chan struct{}), listener: listener,
		proxy: &http.Server{Handler: newPreviewProxy(proxyHost, sessionID, upstream)},
	}
	if err := cmd.Start(); err != nil {
		cancel()
		_ = listener.Close()
		return domain.PreviewSession{}, fmt.Errorf("start project preview: %w", err)
	}
	r.active = process
	go func() { _ = process.proxy.Serve(process.listener) }()
	go func() {
		process.err = cmd.Wait()
		close(process.done)
	}()
	if err := waitForPreview(process, previewStartupTimeout); err != nil {
		_ = r.stopLocked()
		return domain.PreviewSession{}, err
	}
	return previewSession(process), nil
}

func (r *NodePreviewRunner) Stop(root string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil || r.active.root != absRoot {
		return nil
	}
	return r.stopLocked()
}

func (r *NodePreviewRunner) Status(root string) domain.PreviewSession {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return domain.PreviewSession{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil || r.active.root != absRoot || !r.isRunning(r.active) {
		return domain.PreviewSession{}
	}
	return previewSession(r.active)
}

func (r *NodePreviewRunner) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopLocked()
}

func (r *NodePreviewRunner) stopLocked() error {
	if r.active == nil {
		return nil
	}
	process := r.active
	r.active = nil
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	_ = process.proxy.Shutdown(shutdown)
	cancelShutdown()
	process.cancel()
	select {
	case <-process.done:
		return nil
	case <-time.After(5 * time.Second):
		killProcessTree(process.cmd)
		<-process.done
		return nil
	}
}

func (r *NodePreviewRunner) isRunning(process *nodePreviewProcess) bool {
	select {
	case <-process.done:
		return false
	default:
		return true
	}
}

func previewSession(process *nodePreviewProcess) domain.PreviewSession {
	return domain.PreviewSession{URL: process.url + "?" + previewSessionQuery + "=" + process.sessionID, SessionID: process.sessionID, Root: process.root, Active: true}
}

func freeLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func waitForPreview(process *nodePreviewProcess, timeout time.Duration) error {
	client := &http.Client{Timeout: 300 * time.Millisecond}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, _ := http.NewRequest(http.MethodGet, process.upstreamURL, nil)
		response, err := client.Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode < http.StatusInternalServerError {
				return nil
			}
		}
		select {
		case <-process.done:
			return fmt.Errorf("project preview exited before becoming ready: %w", process.err)
		case <-deadline.C:
			return fmt.Errorf("project preview did not become ready within %s", timeout)
		case <-ticker.C:
		}
	}
}
