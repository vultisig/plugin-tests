package queue

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

type Producer struct {
	client *asynq.Client
}

func NewProducer(client *asynq.Client) *Producer {
	return &Producer{client: client}
}

func (p *Producer) EnqueueTestRun(payload TestRunPayload) (*asynq.TaskInfo, error) {
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal test run payload: %w", err)
	}

	info, err := p.client.Enqueue(
		asynq.NewTask(TypeRunIntegrationTest, buf),
		asynq.MaxRetry(0),
		asynq.Timeout(30*time.Minute),
		asynq.Retention(24*time.Hour),
		asynq.Queue(QueueName),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to enqueue test run: %w", err)
	}

	return info, nil
}

func (p *Producer) Close() error {
	return p.client.Close()
}
