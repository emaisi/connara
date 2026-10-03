package workflow

import (
	"apihub-go/internal/jsonutil"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const CodeBuild = "js-v1/goja-104bc28c3abd/protocol-1"
const MaxCodeData = 1 << 20
const CodeTimeout = 2 * time.Second
const CodeWait = 500 * time.Millisecond

type Diagnostic struct {
	Code      string `json:"code"`
	StepID    string `json:"stepId,omitempty"`
	Phase     string `json:"phase"`
	FieldPath string `json:"fieldPath,omitempty"`
	Line      int    `json:"line,omitempty"`
	Column    int    `json:"column,omitempty"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
}
type CodeRequest struct {
	Protocol       int            `json:"protocol"`
	Mode           string         `json:"mode"`
	RequestID      string         `json:"requestId"`
	RuntimeProfile string         `json:"runtimeProfile"`
	Code           string         `json:"code,omitempty"`
	Input          map[string]any `json:"input,omitempty"`
}
type CodeResponse struct {
	Protocol      int            `json:"protocol"`
	RequestID     string         `json:"requestId"`
	Build         string         `json:"build"`
	Success       bool           `json:"success"`
	Output        map[string]any `json:"output"`
	ErrorCode     string         `json:"errorCode,omitempty"`
	Diagnostics   []Diagnostic   `json:"diagnostics,omitempty"`
	LogCount      int            `json:"logCount,omitempty"`
	LogsTruncated bool           `json:"logsTruncated,omitempty"`
	DurationMS    int64          `json:"durationMs"`
}
type CodeError struct {
	Code        string
	Diagnostics []Diagnostic
}

func (e *CodeError) Error() string { return e.Code }
func codeError(code string) error  { return &CodeError{Code: code} }

type codeWaiter struct {
	ready   chan struct{}
	granted bool
}

// One runner is created at service startup, shared by compile, preview and all
// formal runners. Admission purpose is selected by trusted Go callers only.
type CodeRunner struct {
	WorkerPath                      string
	LauncherPath                    string
	Concurrency                     int
	mu                              sync.Mutex
	used, interactive, activeFormal int
	waiters                         []*codeWaiter
	changed                         chan struct{}
	probePassed                     bool
}

func NewCodeRunner(worker, launcher string, concurrency int) *CodeRunner {
	return &CodeRunner{WorkerPath: worker, LauncherPath: launcher, Concurrency: concurrency, changed: make(chan struct{})}
}
func (r *CodeRunner) signal() { close(r.changed); r.changed = make(chan struct{}) }
func (r *CodeRunner) dispatch() {
	for r.used < r.Concurrency && len(r.waiters) > 0 {
		waiter := r.waiters[0]
		r.waiters = r.waiters[1:]
		r.used++
		waiter.granted = true
		close(waiter.ready)
	}
	r.signal()
}
func (r *CodeRunner) acquire(ctx context.Context, formal bool) (func(), error) {
	if r == nil || r.Concurrency < 1 || r.Concurrency > 16 {
		return nil, codeError("code_runtime_unavailable")
	}
	r.mu.Lock()
	if !formal {
		allowed := r.Concurrency - 1
		if r.Concurrency == 1 {
			allowed = 1
		}
		if r.used >= r.Concurrency || r.interactive >= allowed || len(r.waiters) > 0 || r.Concurrency == 1 && r.activeFormal > 0 {
			r.mu.Unlock()
			return nil, codeError("code_capacity_busy")
		}
		r.used++
		r.interactive++
		r.mu.Unlock()
	} else {
		waiter := &codeWaiter{ready: make(chan struct{})}
		r.waiters = append(r.waiters, waiter)
		r.dispatch()
		r.mu.Unlock()
		waitCtx, cancel := context.WithTimeout(ctx, CodeWait)
		defer cancel()
		select {
		case <-waiter.ready:
		case <-waitCtx.Done():
			r.mu.Lock()
			if waiter.granted {
				r.used--
			} else {
				for i, w := range r.waiters {
					if w == waiter {
						r.waiters = append(r.waiters[:i], r.waiters[i+1:]...)
						break
					}
				}
			}
			r.dispatch()
			r.mu.Unlock()
			return nil, codeError("code_capacity_busy")
		}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			r.used--
			if !formal {
				r.interactive--
			}
			r.dispatch()
			r.mu.Unlock()
		})
	}, nil
}
func (r *CodeRunner) BeginFormal(ctx context.Context) (func(), error) {
	if r == nil {
		return nil, codeError("code_runtime_unavailable")
	}
	r.mu.Lock()
	r.activeFormal++
	r.signal()
	r.mu.Unlock()
	var once sync.Once
	release := func() { once.Do(func() { r.mu.Lock(); r.activeFormal--; r.signal(); r.mu.Unlock() }) }
	if r.Concurrency != 1 {
		return release, nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, CodeWait)
	defer cancel()
	for {
		r.mu.Lock()
		busy := r.interactive > 0
		changed := r.changed
		r.mu.Unlock()
		if !busy {
			return release, nil
		}
		select {
		case <-changed:
		case <-waitCtx.Done():
			release()
			return nil, codeError("code_capacity_busy")
		}
	}
}

type boundedWriter struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if len(p) > w.limit-w.Len() {
		w.exceeded = true
		return 0, fmt.Errorf("protocol output exceeds limit")
	}
	return w.Buffer.Write(p)
}

func (r *CodeRunner) invoke(ctx context.Context, request CodeRequest, formal bool) (CodeResponse, error) {
	if r == nil || !filepath.IsAbs(r.WorkerPath) || !filepath.IsAbs(r.LauncherPath) {
		return CodeResponse{}, codeError("code_runtime_unavailable")
	}
	release, err := r.acquire(ctx, formal)
	if err != nil {
		return CodeResponse{}, err
	}
	defer release()
	raw, err := json.Marshal(request)
	if err != nil || len(raw) > MaxCodeData+(40<<10) {
		return CodeResponse{}, codeError("code_input_invalid")
	}
	callCtx, cancel := context.WithTimeout(ctx, CodeTimeout)
	defer cancel()
	command := exec.CommandContext(callCtx, r.LauncherPath, r.WorkerPath)
	command.Env = []string{"TZ=UTC"}
	command.Stdin = bytes.NewReader(raw)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = 100 * time.Millisecond
	stdout := &boundedWriter{limit: MaxCodeData + (8 << 10)}
	stderr := &boundedWriter{limit: 8 << 10}
	command.Stdout = stdout
	command.Stderr = stderr
	runErr := command.Run()
	if command.Process != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		stop := exec.CommandContext(cleanup, "/usr/bin/systemctl", "--user", "stop", fmt.Sprintf("connara-code-%d.service", command.Process.Pid))
		stop.Env = []string{"XDG_RUNTIME_DIR=/run/user/" + fmt.Sprint(os.Getuid()), "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/" + fmt.Sprint(os.Getuid()) + "/bus"}
		_ = stop.Run()
	}
	if runErr != nil {
		if callCtx.Err() != nil {
			return CodeResponse{}, codeError("code_timeout")
		}
		return CodeResponse{}, codeError("code_worker_failed")
	}
	if stdout.exceeded || stderr.exceeded {
		return CodeResponse{}, codeError("code_worker_failed")
	}
	var response CodeResponse
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return response, codeError("code_worker_failed")
	}
	if decoder.Decode(new(any)) != io.EOF || response.Protocol != 1 || response.RequestID != request.RequestID || response.Build != CodeBuild {
		return response, codeError("code_worker_failed")
	}
	if !response.Success {
		return response, &CodeError{Code: response.ErrorCode, Diagnostics: response.Diagnostics}
	}
	return response, nil
}
func (r *CodeRunner) Probe(ctx context.Context) error {
	_, err := r.invoke(ctx, CodeRequest{Protocol: 1, Mode: "probe", RequestID: "probe", RuntimeProfile: "js-v1"}, false)
	if r != nil && err == nil {
		r.mu.Lock()
		r.probePassed = true
		r.mu.Unlock()
	}
	return err
}
func (r *CodeRunner) EnvironmentAvailable() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.probePassed
}
func (r *CodeRunner) Preflight(ctx context.Context) error {
	// Formal preflight checks the fixed sandbox even after new admission has
	// been disabled. It never compiles or runs user source.
	_, err := r.invoke(ctx, CodeRequest{Protocol: 1, Mode: "probe", RequestID: "formal-probe", RuntimeProfile: "js-v1"}, true)
	return err
}
func (r *CodeRunner) Compile(ctx context.Context, step Step, formal bool) error {
	_, err := r.invoke(ctx, CodeRequest{Protocol: 1, Mode: "compile", RequestID: step.ID, RuntimeProfile: step.RuntimeProfile, Code: step.Code}, formal)
	if err != nil {
		var typed *CodeError
		if errors.As(err, &typed) {
			if len(typed.Diagnostics) == 0 {
				typed.Diagnostics = []Diagnostic{{Code: typed.Code, StepID: step.ID, Phase: "code", FieldPath: "/code", Severity: "error", Message: typed.Code}}
			}
			for i := range typed.Diagnostics {
				typed.Diagnostics[i].StepID = step.ID
				if typed.Diagnostics[i].FieldPath == "" {
					typed.Diagnostics[i].FieldPath = "/code"
				}
			}
		}
	}
	return err
}
func (r *CodeRunner) Execute(ctx context.Context, step StepSnapshot, input map[string]any, formal bool) (CodeResponse, error) {
	if step.RunnerBuild != "" && step.RunnerBuild != CodeBuild {
		return CodeResponse{}, codeError("code_runtime_unavailable")
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > MaxCodeData {
		return CodeResponse{}, codeError("code_input_invalid")
	}
	var normalized map[string]any
	if err := jsonutil.Unmarshal(raw, &normalized); err != nil {
		return CodeResponse{}, codeError("code_input_invalid")
	}
	if err := ValidateTrigger(step.InputSchema, normalized); err != nil {
		diagnostics := FieldDiagnostic(err, step.ID, "code", "/input")
		for i := range diagnostics {
			diagnostics[i].Code = "code_input_invalid"
		}
		return CodeResponse{Diagnostics: diagnostics}, &CodeError{Code: "code_input_invalid", Diagnostics: diagnostics}
	}
	response, err := r.invoke(ctx, CodeRequest{Protocol: 1, Mode: "execute", RequestID: step.ID, RuntimeProfile: step.RuntimeProfile, Code: step.Code, Input: normalized}, formal)
	if err != nil {
		if len(response.Diagnostics) == 0 {
			path := "/code"
			if CodeErrorCode(err) == "code_input_invalid" {
				path = "/input"
			}
			if CodeErrorCode(err) == "code_output_invalid" {
				path = "/outputSchema"
			}
			response.Diagnostics = []Diagnostic{{Code: CodeErrorCode(err), StepID: step.ID, Phase: "code", FieldPath: path, Severity: "error", Message: CodeErrorCode(err)}}
		}
		for i := range response.Diagnostics {
			response.Diagnostics[i].StepID = step.ID
			if response.Diagnostics[i].FieldPath == "" {
				response.Diagnostics[i].FieldPath = "/code"
			}
		}
		return response, err
	}
	raw, err = json.Marshal(response.Output)
	if err != nil || len(raw) > MaxCodeData || response.Output == nil {
		return response, codeError("code_output_invalid")
	}
	if err := ValidateTrigger(step.OutputSchema, response.Output); err != nil {
		response.Diagnostics = FieldDiagnostic(err, step.ID, "code", "/outputSchema")
		for i := range response.Diagnostics {
			response.Diagnostics[i].Code = "code_output_invalid"
		}
		return response, &CodeError{Code: "code_output_invalid", Diagnostics: response.Diagnostics}
	}
	return response, nil
}
func CodeErrorCode(err error) string {
	var typed *CodeError
	if errors.As(err, &typed) {
		return typed.Code
	}
	return "code_execution_failed"
}

// CheckJSNumber tests exact integer bounds before any Number conversion.
func CheckJSNumber(value json.Number) error {
	number, err := exactNumber(value)
	if err != nil {
		return err
	}
	if number.IsInt() && new(big.Int).Abs(number.Num()).Cmp(big.NewInt(9007199254740991)) > 0 {
		return codeError("code_input_invalid")
	}
	f, err := value.Float64()
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) || math.Trunc(f) == f && math.Abs(f) > 9007199254740991 {
		return codeError("code_input_invalid")
	}
	return nil
}
