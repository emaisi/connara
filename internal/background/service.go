package background

import (
	"apihub-go/internal/jsonutil"
	"apihub-go/internal/webhooksig"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"apihub-go/internal/authn"
	"apihub-go/internal/catalog"
	"apihub-go/internal/executor"
	"apihub-go/internal/model"
	"apihub-go/internal/secret"
	"apihub-go/internal/store"
)

type Service struct {
	store    *store.Store
	auth     *authn.Service
	executor *executor.Executor
	codec    *secret.Codec
	client   *http.Client
	logger   *slog.Logger
	workerID string
	stopping atomic.Bool
}

func New(database *store.Store, auth *authn.Service, actionExecutor *executor.Executor, codec *secret.Codec, client *http.Client, logger *slog.Logger, workerID string) *Service {
	return &Service{store: database, auth: auth, executor: actionExecutor, codec: codec, client: client, logger: logger, workerID: fmt.Sprintf("%s:%x", workerID, randomWorkerID())}
}

func (s *Service) RunWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for !s.stopping.Load() {
		if err := s.work(ctx); err != nil && !errors.Is(err, context.Canceled) {
			s.logger.ErrorContext(ctx, "background worker", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) RunScheduler(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	nextCleanup := time.Time{}
	for !s.stopping.Load() {
		if time.Now().UTC().After(nextCleanup) {
			removed, err := s.store.CleanupExpired(ctx)
			if err != nil && !errors.Is(err, context.Canceled) {
				s.logger.ErrorContext(ctx, "cleanup expired data", "error", err)
			} else if removed > 0 {
				s.logger.InfoContext(ctx, "cleaned expired data", "rows", removed)
			}
			nextCleanup = time.Now().UTC().Add(time.Minute)
		}
		if _, err := s.store.EnqueueDueSyncTasks(ctx, 20); err != nil && !errors.Is(err, context.Canceled) {
			s.logger.ErrorContext(ctx, "enqueue scheduled sync", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) work(ctx context.Context) error {
	if _, err := s.store.ExpandOutbox(ctx, 50); err != nil {
		return err
	}

	if s.stopping.Load() {
		return nil
	}
	jobs, err := s.store.ClaimJobs(ctx, s.workerID, 1)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		jobCtx, cancel := context.WithCancel(ctx)
		heartbeatDone := make(chan struct{})
		go func() {
			defer close(heartbeatDone)
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-jobCtx.Done():
					return
				case <-ticker.C:
					renewCtx, stop := context.WithTimeout(jobCtx, 5*time.Second)
					err := s.store.RenewJob(renewCtx, job)
					stop()
					if err != nil {
						s.logger.Error("job lease lost", "job_id", job.ID, "error", err)
						cancel()
						return
					}
				}
			}
		}()
		var runErr error
		switch job.Kind {
		case "sync_run":
			runErr = s.runSync(jobCtx, job)
		case "webhook_delivery":
			runErr = s.deliverWebhook(jobCtx, job)
		default:
			runErr = fmt.Errorf("unsupported job kind %q", job.Kind)
		}
		cancel()
		<-heartbeatDone
		finishCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		var finishErr error
		if runErr == nil {
			finishErr = s.store.CompleteJob(finishCtx, job, s.workerID)
		} else {
			finishErr = s.store.FailJob(finishCtx, job, s.workerID, runErr.Error())
			s.logger.Warn("background job failed", "job_id", job.ID, "attempt", job.Attempt, "error", runErr)
		}
		stop()
		if finishErr != nil {
			return fmt.Errorf("finalize job %s: %w", job.ID, finishErr)
		}
	}

	return nil
}

func (s *Service) runSync(ctx context.Context, job model.Job) error {
	task, err := s.store.SyncTask(ctx, job.ResourceID)
	if err != nil {
		return err
	}
	if task.Status != "deployed" {
		return fmt.Errorf("sync task is not deployed")
	}
	if err := s.store.ValidateSyncTaskReady(ctx, task); err != nil {
		return err
	}
	action, err := s.store.Action(ctx, task.ActionID)
	if err != nil {
		return err
	}
	integration, err := s.store.Integration(ctx, task.IntegrationID)
	if err != nil {
		return err
	}
	resolved, err := s.auth.Resolve(ctx, task.ConnectionID)
	if err != nil {
		return err
	}
	provider := model.Provider{Service: action.SystemKey, DisplayName: action.SystemKey, BaseURL: integration.BaseURL}

	config, err := ParseSyncConfig(task.SyncConfig)
	if err != nil {
		return err
	}
	var checkpoint struct {
		OperationID string `json:"operationId"`
		Cursor      string `json:"cursor"`
		Completed   bool   `json:"completed"`
		Records     int    `json:"records"`
		Pages       int    `json:"pages"`
		TaskVersion int64  `json:"taskVersion"`
	}
	if err := jsonutil.Unmarshal(task.Checkpoint, &checkpoint); err != nil {
		return err
	}
	if checkpoint.OperationID == job.OperationID && checkpoint.Completed {
		return nil
	}
	if checkpoint.OperationID != job.OperationID {
		checkpoint.OperationID = job.OperationID
		checkpoint.Cursor = ""
		checkpoint.Completed = false
		checkpoint.Records = 0
		checkpoint.Pages = 0
		checkpoint.TaskVersion = task.Version
	}
	if checkpoint.TaskVersion != task.Version {
		return errors.New("sync configuration changed during run; enqueue a new run")
	}
	input := map[string]any{}
	if err := jsonutil.Unmarshal(task.Input, &input); err != nil {
		return err
	}
	if err := s.store.MarkOperationRunning(ctx, job.OperationID); err != nil {
		return err
	}
	validator := &catalog.Catalog{}
	seen := map[string]bool{}
	for checkpoint.Pages < config.MaxPages {
		if config.CursorParam != "" {
			if checkpoint.Cursor != "" {
				input[config.CursorParam] = checkpoint.Cursor
			}
		}
		if config.PageSizeParam != "" && config.PageSize > 0 {
			input[config.PageSizeParam] = json.Number(strconv.Itoa(config.PageSize))
		}
		if err := validator.ValidateInput(actionModel(action), input); err != nil {
			return err
		}
		result, err := s.executor.Action(ctx, provider, actionModel(action), input, nil, executor.RequestAuth{Headers: resolved.Headers, Query: resolved.Query, Cookies: resolved.Cookies})
		if err != nil {
			return err
		}
		if result.Status < 200 || result.Status >= 300 {
			return fmt.Errorf("sync provider returned HTTP %d", result.Status)
		}
		records, next, err := extractSyncPage(task.ID, result.Body, config)
		if err != nil {
			return err
		}
		if next != "" && (next == checkpoint.Cursor || seen[next]) {
			return errors.New("sync provider repeated a pagination cursor")
		}
		seen[next] = true
		checkpoint.Cursor = next
		checkpoint.Completed = next == ""
		checkpoint.Records += len(records)
		checkpoint.Pages++
		encoded := store.MarshalJSON(checkpoint)
		if err := s.store.CommitSyncPage(ctx, job, task, encoded, records, checkpoint.Completed, checkpoint.Records); err != nil {
			return err
		}
		task.Checkpoint = encoded
		if checkpoint.Completed {
			return nil
		}
	}
	return errors.New("sync maxPages reached; inspect provider pagination before retrying")
}

func (s *Service) deliverWebhook(ctx context.Context, job model.Job) error {
	work, err := s.store.WebhookDeliveryWork(ctx, job.ResourceID)
	if err != nil {
		return err
	}
	if work.Status == "delivered" {
		return nil
	}
	targetURL := work.TargetURL
	if work.Attempt > 0 && work.FallbackURL != "" {
		targetURL = work.FallbackURL
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(work.Payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "apihub-go/0.2")
	request.Header.Set("X-APIHub-Event", work.EventType)
	request.Header.Set("X-APIHub-Event-ID", work.EventID)
	if len(work.SecretBlob) > 0 {
		plain, decryptErr := s.codec.Decrypt(work.SecretBlob, []byte(s.store.WorkspaceID()+":webhook-endpoint:"+work.EndpointID))
		if decryptErr != nil {
			return decryptErr
		}
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		request.Header.Set("X-APIHub-Timestamp", timestamp)
		request.Header.Set("X-APIHub-Signature", webhooksig.Sign(plain, timestamp, work.EventID, work.EventType, work.Payload))
	}
	attempt := model.WebhookDeliveryAttempt{
		WebhookDeliveryID: work.DeliveryID,
		AttemptNo:         work.Attempt + 1,
		TargetURL:         targetURL,
		RequestID:         job.ID,
	}
	startedAt := time.Now()
	response, err := s.client.Do(request)
	attempt.DurationMS = time.Since(startedAt).Milliseconds()
	if err != nil {
		attempt.ErrorMessage = err.Error()
		if recordErr := s.store.FinishWebhookDeliveryAttempt(ctx, job, attempt, false, job.Attempt >= job.MaxAttempts); recordErr != nil {
			return fmt.Errorf("record webhook delivery failure: %w", recordErr)
		}
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		err = fmt.Errorf("webhook target returned HTTP %d", response.StatusCode)
		attempt.HTTPStatus = response.StatusCode
		attempt.ErrorMessage = err.Error()
		if recordErr := s.store.FinishWebhookDeliveryAttempt(ctx, job, attempt, false, job.Attempt >= job.MaxAttempts); recordErr != nil {
			return fmt.Errorf("record webhook delivery failure: %w", recordErr)
		}
		return err
	}
	attempt.HTTPStatus = response.StatusCode
	return s.store.FinishWebhookDeliveryAttempt(ctx, job, attempt, true, false)
}

func actionModel(item model.ActionDefinition) model.Action {
	input, output := map[string]any{}, map[string]any{}
	_ = jsonutil.Unmarshal(item.InputSchema, &input)
	_ = jsonutil.Unmarshal(item.OutputSchema, &output)
	return model.Action{ID: item.ActionKey, Service: item.SystemKey, Name: item.Name, Description: item.Description, RequiredScopes: item.RequiredScopes, InputSchema: input, OutputSchema: output, Runtime: &model.HTTPActionRuntime{Method: item.HTTPMethod, Path: item.RelativePath}, Executable: item.Executable}
}

func randomWorkerID() []byte {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		panic(err)
	}
	return id
}
func (s *Service) StopClaiming() { s.stopping.Store(true) }
