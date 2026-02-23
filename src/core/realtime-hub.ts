export interface RealtimeEvent {
  event: string;
  payload: unknown;
}

type RealtimeSubscriber = (event: RealtimeEvent) => void;

export class ProjectRealtimeHub {
  private readonly subscribers = new Map<string, Set<RealtimeSubscriber>>();

  subscribe(projectId: string, subscriber: RealtimeSubscriber): () => void {
    const listeners = this.subscribers.get(projectId) ?? new Set<RealtimeSubscriber>();
    listeners.add(subscriber);
    this.subscribers.set(projectId, listeners);

    return () => {
      const current = this.subscribers.get(projectId);
      if (!current) {
        return;
      }

      current.delete(subscriber);
      if (current.size === 0) {
        this.subscribers.delete(projectId);
      }
    };
  }

  publish(projectId: string, event: RealtimeEvent): void {
    const listeners = this.subscribers.get(projectId);
    if (!listeners || listeners.size === 0) {
      return;
    }

    for (const listener of listeners) {
      listener(event);
    }
  }

  getSubscriberCount(projectId: string): number {
    return this.subscribers.get(projectId)?.size ?? 0;
  }
}

export function createProjectRealtimeHub(): ProjectRealtimeHub {
  return new ProjectRealtimeHub();
}

export function publishProjectDirty(hub: ProjectRealtimeHub, projectId: string): void {
  hub.publish(projectId, {
    event: "project_dirty",
    payload: {
      type: "project_dirty",
      transport: "sse",
      timestamp: new Date().toISOString(),
      projectId,
    },
  });
}
