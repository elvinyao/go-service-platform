package workflow

import (
	"project/internal/manager"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/logger"
	// 其他必要的导入
)

type WorkflowA struct {
	name           string
	serviceManager *manager.ServiceManager
}

func NewWorkflowA(sm *manager.ServiceManager) *WorkflowA {
	return &WorkflowA{
		name:           "WorkflowA",
		serviceManager: sm,
	}
}

func (w *WorkflowA) GetName() string {
	return w.name
}

func (w *WorkflowA) ProcessMessage(msg model.Message) error {
	logger.Infof("Processing message in %s: %v", w.name, msg)

	// 获取对应的服务
	confluenceServices := w.serviceManager.GetServicesByWorkflowAndType("A", "confluence")
	badgeDBService, _ := w.serviceManager.GetServiceByName("BadgeDBService")

	// 查询BadgeDB获取处理方式
	processingMethod, err := badgeDBService.(*service.BadgeDBService).GetProcessingMethod(msg.Type)
	if err != nil {
		return err
	}

	// 根据处理方式调用ConfluenceService
	for _, s := range confluenceServices {
		if s.GetName() == processingMethod {
			confluenceService := s.(*service.ConfluenceService)
			data, err := confluenceService.FetchData()
			if err != nil {
				return err
			}
			// 进行指定格式的日志记录
			logger.Infof("Data fetched: %v", data)
			break
		}
	}

	return nil
}
