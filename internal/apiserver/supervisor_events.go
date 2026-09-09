package apiserver

import (
	"context"
	"encoding/json"
	"github.com/huynle/brain-api/internal/bridge"
	"github.com/huynle/brain-api/internal/types"
	"sync"
	"time"
)

type supervisorInstanceReader interface {
	ListInstances(context.Context, string) (*types.InstanceListResponse, error)
}
type supervisorEventWriter interface {
	Ingest(context.Context, []types.Event) error
}

// Reuse always-on bridge control frames, without creating an executor poller.
// A bounded queue protects the bridge. These hints are lossy; snapshots are authoritative.
func wireSupervisorControlEvents(ctx context.Context, hub *bridge.Hub, instances supervisorInstanceReader, events supervisorEventWriter) {
	type observation struct {
		runner, instance string
		event            types.Event
	}
	queue := make(chan observation, 128)
	var mu sync.Mutex
	last := map[string]time.Time{}
	hub.SetControlObserver(func(runner, instance string, raw json.RawMessage) {
		event, ok := projectSupervisorControl(raw)
		if !ok {
			return
		}
		key := runner + "/" + instance
		now := time.Now()
		if event.Type == types.EventSessionActivity {
			mu.Lock()
			previous := last[key]
			if now.Sub(previous) < 5*time.Second {
				mu.Unlock()
				return
			}
			if len(last) >= 1000 {
				for k := range last {
					delete(last, k)
					break
				}
			}
			last[key] = now
			if len(last) > 1000 {
				for k, at := range last {
					if now.Sub(at) > time.Minute {
						delete(last, k)
					}
				}
			}
			mu.Unlock()
		}
		select {
		case queue <- observation{runner, instance, event}:
		default: /* The retained event ring is already lossy; snapshots remain authoritative. */
		}
	})
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case observed := <-queue:
				readCtx, cancel := context.WithTimeout(ctx, time.Second)
				list, err := instances.ListInstances(readCtx, observed.runner)
				var instance *types.OpencodeInstance
				if list != nil {
					for i := range list.Instances {
						if list.Instances[i].InstanceID == observed.instance {
							instance = &list.Instances[i]
							break
						}
					}
				}
				cancel()
				if err != nil || instance == nil || instance.ProjectID == "" {
					continue
				}
				event := observed.event
				event.RunnerID = observed.runner
				event.ProjectID = instance.ProjectID
				event.TaskID = instance.TaskID
				event.FeatureID = instance.FeatureID
				event.Metadata["instance_id"] = observed.instance
				_ = events.Ingest(ctx, []types.Event{event})
			}
		}
	}()
}
func projectSupervisorControl(raw json.RawMessage) (types.Event, bool) {
	var frame struct {
		Type       string `json:"type"`
		Properties struct {
			ID        string `json:"id"`
			SessionID string `json:"sessionID"`
			Info      struct {
				SessionID string `json:"sessionID"`
			} `json:"info"`
		} `json:"properties"`
	}
	if json.Unmarshal(raw, &frame) != nil {
		return types.Event{}, false
	}
	kind := types.EventSessionActivity
	switch frame.Type {
	case "permission.updated", "permission.asked":
		kind = types.EventSessionPermission
	case "session.idle", "session.error", "session.status", "session.updated", "message.updated":
	default:
		return types.Event{}, false
	}
	session := frame.Properties.SessionID
	if session == "" {
		session = frame.Properties.Info.SessionID
	}
	if len(session) > 256 || len(frame.Properties.ID) > 256 {
		return types.Event{}, false
	}
	metadata := map[string]string{"session_id": session}
	if kind == types.EventSessionPermission {
		metadata["permission_id"] = frame.Properties.ID
	}
	return types.Event{Type: kind, Source: types.EventSourceAPI, Metadata: metadata}, true
}
