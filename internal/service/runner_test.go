package service

import (
	"context"
	"testing"
)

func TestRunnerService_InitialStatus(t *testing.T) {
	svc := NewRunnerService()
	ctx := context.Background()

	status, err := svc.GetStatus(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Running {
		t.Error("expected Running to be true")
	}
	if status.Paused {
		t.Error("expected Paused to be false initially")
	}
	if len(status.PausedProjects) != 0 {
		t.Errorf("expected no paused projects, got %d", len(status.PausedProjects))
	}
}

func TestRunnerService_PauseProject(t *testing.T) {
	svc := NewRunnerService()
	ctx := context.Background()

	if err := svc.Pause(ctx, "brain-api"); err != nil {
		t.Fatalf("pause failed: %v", err)
	}

	status, err := svc.GetStatus(ctx)
	if err != nil {
		t.Fatalf("get status failed: %v", err)
	}
	if !status.Paused {
		t.Error("expected Paused to be true after pausing a project")
	}
	if len(status.PausedProjects) != 1 {
		t.Fatalf("expected 1 paused project, got %d", len(status.PausedProjects))
	}
	if status.PausedProjects[0] != "brain-api" {
		t.Errorf("expected brain-api in paused projects, got %s", status.PausedProjects[0])
	}
}

func TestRunnerService_ResumeProject(t *testing.T) {
	svc := NewRunnerService()
	ctx := context.Background()

	_ = svc.Pause(ctx, "brain-api")
	if err := svc.Resume(ctx, "brain-api"); err != nil {
		t.Fatalf("resume failed: %v", err)
	}

	status, _ := svc.GetStatus(ctx)
	if status.Paused {
		t.Error("expected Paused to be false after resume")
	}
	if len(status.PausedProjects) != 0 {
		t.Errorf("expected no paused projects, got %d", len(status.PausedProjects))
	}
}

func TestRunnerService_PauseAll(t *testing.T) {
	svc := NewRunnerService()
	ctx := context.Background()

	if err := svc.PauseAll(ctx); err != nil {
		t.Fatalf("pause all failed: %v", err)
	}

	status, _ := svc.GetStatus(ctx)
	if !status.Paused {
		t.Error("expected Paused to be true after PauseAll")
	}
}

func TestRunnerService_ResumeAll(t *testing.T) {
	svc := NewRunnerService()
	ctx := context.Background()

	_ = svc.Pause(ctx, "brain-api")
	_ = svc.PauseAll(ctx)

	if err := svc.ResumeAll(ctx); err != nil {
		t.Fatalf("resume all failed: %v", err)
	}

	status, _ := svc.GetStatus(ctx)
	if status.Paused {
		t.Error("expected Paused to be false after ResumeAll")
	}
	if len(status.PausedProjects) != 0 {
		t.Errorf("expected no paused projects after ResumeAll, got %d", len(status.PausedProjects))
	}
}

func TestRunnerService_IsPaused(t *testing.T) {
	svc := NewRunnerService()
	ctx := context.Background()

	if svc.IsPaused("brain-api") {
		t.Error("expected not paused initially")
	}

	_ = svc.Pause(ctx, "brain-api")
	if !svc.IsPaused("brain-api") {
		t.Error("expected paused after Pause")
	}
	if svc.IsPaused("other-project") {
		t.Error("expected other project not paused")
	}

	_ = svc.PauseAll(ctx)
	if !svc.IsPaused("other-project") {
		t.Error("expected all projects paused after PauseAll")
	}
}
