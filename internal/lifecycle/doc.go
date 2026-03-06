// Package lifecycle provides primitives for server process lifecycle management.
//
// This package implements:
//   - PID file management (write, read, clear, check running)
//   - Process daemonization (spawn detached background processes)
//   - Signal handling (SIGTERM, SIGINT, SIGHUP with callbacks)
//   - Server state tracking
//
// # PID Management
//
// PID files are used to track running server processes:
//
//	WritePID("/var/run/server.pid", os.Getpid())
//	pid, err := ReadPID("/var/run/server.pid")
//	if IsProcessRunning(pid) {
//		fmt.Println("Server is running")
//	}
//	ClearPID("/var/run/server.pid")
//
// # Daemonization
//
// Spawn a process as a detached background daemon:
//
//	opts := DaemonOptions{
//		PIDFile: "/var/run/server.pid",
//		LogFile: "/var/log/server.log",
//		WorkDir: "/app",
//	}
//	pid, err := Daemonize("./server", []string{"--port", "8080"}, opts)
//
// # Signal Handling
//
// Handle OS signals with custom callbacks:
//
//	ctx := context.Background()
//	handler := SetupSignalHandler(ctx, SignalHandlerOptions{
//		OnShutdown: func() {
//			log.Println("Shutting down gracefully...")
//		},
//		OnReload: func() {
//			log.Println("Reloading configuration...")
//		},
//		GracefulTimeout: 30 * time.Second,
//	})
//
//	if handler.IsShuttingDown() {
//		// Cleanup before exit
//	}
package lifecycle
