package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/elvinyao/go-service-platform/internal/manager"
	"github.com/elvinyao/go-service-platform/internal/service"
)

type stopRecorder struct {
	mu    sync.Mutex
	order []string
}

func (r *stopRecorder) record(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, name)
}

type recordingStopService struct {
	appTestService
	recorder *stopRecorder
	err      error
}

func (s recordingStopService) Stop(context.Context) error {
	s.recorder.record(s.name)
	return s.err
}

func TestAppStopStopsInputBeforeOtherServices(t *testing.T) {
	recorder := &stopRecorder{}
	sm := manager.NewServiceManager("test")
	sm.RegisterService(recordingStopService{appTestService: appTestService{name: "output"}, recorder: recorder})
	sm.RegisterService(recordingStopService{appTestService: appTestService{name: service.WebSocketInputServiceName}, recorder: recorder})
	application := &App{serviceManager: sm, state: AppStateRunning}

	if err := application.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if len(recorder.order) == 0 || recorder.order[0] != service.WebSocketInputServiceName {
		t.Fatalf("stop order = %v, want the input stopped first", recorder.order)
	}
}

func TestStopInputsReportsFailureAndToleratesMissingInput(t *testing.T) {
	if err := stopInputs(context.Background(), manager.NewServiceManager("test")); err != nil {
		t.Fatalf("stop inputs without an input service: %v", err)
	}

	sm := manager.NewServiceManager("test")
	sm.RegisterService(recordingStopService{
		appTestService: appTestService{name: service.WebSocketInputServiceName},
		recorder:       &stopRecorder{},
		err:            errors.New("boom"),
	})
	err := stopInputs(context.Background(), sm)
	if err == nil || !strings.Contains(err.Error(), "stop input") {
		t.Fatalf("error = %v, want an input stop error", err)
	}
}
