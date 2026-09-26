package email

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	taskspb "cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type TaskDispatcherConfig struct {
	Project        string
	Location       string
	Queue          string
	WorkerURL      string
	ServiceAccount string
	Audience       string
	Timeout        time.Duration
}

type TaskDispatcher struct {
	client *cloudtasks.Client
	config TaskDispatcherConfig
	parent string
	logger *slog.Logger
}

func NewTaskDispatcher(ctx context.Context, config TaskDispatcherConfig, logger *slog.Logger) (*TaskDispatcher, error) {
	client, err := cloudtasks.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create cloud tasks client: %w", err)
	}
	config.WorkerURL = strings.TrimRight(config.WorkerURL, "/")
	config.Audience = strings.TrimRight(config.Audience, "/")
	return &TaskDispatcher{
		client: client, config: config,
		parent: fmt.Sprintf("projects/%s/locations/%s/queues/%s", config.Project, config.Location, config.Queue),
		logger: logger,
	}, nil
}

func (dispatcher *TaskDispatcher) Close() error { return dispatcher.client.Close() }

func (dispatcher *TaskDispatcher) Notify(ctx context.Context, outboxID int64) {
	dispatcher.notify(ctx, "email", outboxID, "/internal/tasks/email-outbox", "outbox_id")
}

func (dispatcher *TaskDispatcher) NotifyMarketing(ctx context.Context, syncID int64) {
	dispatcher.notify(ctx, "marketing", syncID, "/internal/tasks/marketing-sync", "sync_id")
}

func (dispatcher *TaskDispatcher) notify(ctx context.Context, kind string, id int64, path, bodyKey string) {
	dispatchCtx, cancel := context.WithTimeout(ctx, dispatcher.config.Timeout)
	defer cancel()
	if err := dispatcher.dispatch(dispatchCtx, kind, id, path, bodyKey); err != nil {
		dispatcher.logger.Error(kind+"_task_enqueue_failed", "item_id", id, "error", err)
	}
}

func (dispatcher *TaskDispatcher) Dispatch(ctx context.Context, outboxID int64) error {
	return dispatcher.dispatch(ctx, "email", outboxID, "/internal/tasks/email-outbox", "outbox_id")
}

func (dispatcher *TaskDispatcher) DispatchMarketing(ctx context.Context, syncID int64) error {
	return dispatcher.dispatch(ctx, "marketing", syncID, "/internal/tasks/marketing-sync", "sync_id")
}

func (dispatcher *TaskDispatcher) dispatch(ctx context.Context, kind string, id int64, path, bodyKey string) error {
	body, err := json.Marshal(map[string]int64{bodyKey: id})
	if err != nil {
		return fmt.Errorf("marshal %s task: %w", kind, err)
	}
	taskName := fmt.Sprintf("%s/tasks/%s-%d", dispatcher.parent, kind, id)
	_, err = dispatcher.client.CreateTask(ctx, &taskspb.CreateTaskRequest{
		Parent: dispatcher.parent,
		Task: &taskspb.Task{
			Name: taskName,
			MessageType: &taskspb.Task_HttpRequest{HttpRequest: &taskspb.HttpRequest{
				HttpMethod: taskspb.HttpMethod_POST,
				Url:        dispatcher.config.WorkerURL + path,
				Headers:    map[string]string{"Content-Type": "application/json"},
				Body:       body,
				AuthorizationHeader: &taskspb.HttpRequest_OidcToken{OidcToken: &taskspb.OidcToken{
					ServiceAccountEmail: dispatcher.config.ServiceAccount,
					Audience:            dispatcher.config.Audience,
				}},
			}},
		},
	})
	if status.Code(err) == codes.AlreadyExists {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create %s task: %w", kind, err)
	}
	dispatcher.logger.Info(kind+"_task_enqueued", "item_id", id)
	return nil
}
