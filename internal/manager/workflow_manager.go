package manager

import (
	"project/internal/interfaces"
	"project/internal/model"
	"project/pkg/logger"
	"sync"
)

type WorkflowManager struct {
	workflows map[string]interfaces.Workflow
	mu        sync.Mutex
}

func NewWorkflowManager() *WorkflowManager {
	return &WorkflowManager{
		workflows: make(map[string]interfaces.Workflow),
	}
}

func (wm *WorkflowManager) RegisterWorkflow(w interfaces.Workflow) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.workflows[w.GetName()] = w
}

func (wm *WorkflowManager) DispatchMessage(msg model.Message) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	for _, w := range wm.workflows {
		// 根据消息类型或其他条件，选择合适的工作流
		if err := w.ProcessMessage(msg); err != nil {
			logger.Errorf("Error processing message in %s: %v", w.GetName(), err)
		}
	}
}
