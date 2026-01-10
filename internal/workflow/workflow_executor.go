package workflow

import "project/pkg/concurrency"

type WorkflowExecutor struct {
	workerPool *concurrency.WorkerPool
	workflows  map[string]*WorkflowDefinition
}

// Implementation methods
