package service

import (
	"testing"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/storage/storagetest"
	"github.com/huynle/brain-api/internal/tenant"
)

func TestWorkloadConstructorsRetainTenantView(t *testing.T) {
	view, err := storagetest.New(t.TempDir() + "/brain.db")
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	cfg := &config.Config{BrainDir: t.TempDir()}
	idx := indexer.NewIndexer(cfg.BrainDir, view)
	brain := NewBrainService(cfg, view, idx, nil, nil)
	tasks := NewTaskService(cfg, view, idx)
	for name, got := range map[string]*storage.TenantStore{
		"brain":                    brain.storage,
		"tasks":                    tasks.storage,
		"runner":                   NewRunnerServiceWithStorage(view).store,
		"runner workload registry": NewRunnerRegistryService(view).storage,
		"client context":           NewClientContextService(view).storage,
		"attachments":              NewAttachmentService(view, nil, nil, 0).storage,
		"webhook":                  NewWebhookService(view).store,
		"webhook custom client":    NewWebhookServiceWithClient(view, nil).store,
		"goal":                     NewGoalService(brain, tasks, view).store,
		"reminder":                 NewReminderService(brain, view).store,
		"placement":                NewProjectPlacementService(view).store,
		"trigger":                  NewTriggerTaskStoreAdapter(view).store,
	} {
		if got != view || got.TenantID() != tenant.Local {
			t.Errorf("%s did not retain the supplied tenant view", name)
		}
	}
}
