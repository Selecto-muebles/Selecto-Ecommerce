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

func NewTaskDispatcher(
	ctx context.Context,
	config TaskDispatcherConfig,
	logger *slog.Logger,
) (*TaskDispatcher, error) {
	client, err := cloudtasks.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create cloud tasks client: %w", err)
	}
	config.WorkerURL = strings.TrimRight(config.WorkerURL, "/")
	config.Audience = strings.TrimRight(config.Audience, "/")
	return &TaskDispatcher{
		client: client,
		config: config,
		parent: fmt.Sprintf(
			"projects/%s/locations/%s/queues/%s",
			config.Project,
			config.Location,
			config.Queue,
		),
		logger: logger,
	}, nil
}

func (d *TaskDispatcher) Close() error {
	return d.client.Close()
}

func (d *TaskDispatcher) Notify(ctx context.Context, outboxID int64) {
	dispatchCtx, cancel := context.WithTimeout(ctx, d.config.Timeout)
	defer cancel()
	if err := d.Dispatch(dispatchCtx, outboxID); err != nil {
		d.logger.Error("email_task_enqueue_failed", "email_id", outboxID, "error", err)
	}
}

func (d *TaskDispatcher) Dispatch(ctx context.Context, outboxID int64) error {
	body, err := json.Marshal(map[string]int64{"outbox_id": outboxID})
	if err != nil {
		return fmt.Errorf("marshal email task: %w", err)
	}
	taskName := fmt.Sprintf("%s/tasks/email-%d", d.parent, outboxID)
	_, err = d.client.CreateTask(ctx, &taskspb.CreateTaskRequest{
		Parent: d.parent,
		Task: &taskspb.Task{
			Name: taskName,
			MessageType: &taskspb.Task_HttpRequest{HttpRequest: &taskspb.HttpRequest{
				HttpMethod: taskspb.HttpMethod_POST,
				Url:        d.config.WorkerURL + "/internal/tasks/email-outbox",
				Headers:    map[string]string{"Content-Type": "application/json"},
				Body:       body,
				AuthorizationHeader: &taskspb.HttpRequest_OidcToken{OidcToken: &taskspb.OidcToken{
					ServiceAccountEmail: d.config.ServiceAccount,
					Audience:            d.config.Audience,
				}},
			}},
		},
	})
	if status.Code(err) == codes.AlreadyExists {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create email task: %w", err)
	}
	d.logger.Info("email_task_enqueued", "email_id", outboxID)
	return nil
}