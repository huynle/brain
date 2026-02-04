#!/usr/bin/env bun
/**
 * Brain API Server Entry Point
 *
 * This is the main entry point for the Brain API server.
 * It starts the Hono-based REST API for AI agent memory and knowledge management.
 *
 * Usage:
 *   brain-server           Start the server
 *   brain-server --help    Show help
 *
 * Environment:
 *   BRAIN_PORT    Server port (default: 3333)
 *   BRAIN_HOST    Server host (default: 0.0.0.0)
 *   BRAIN_DIR     Brain data directory (default: ~/.brain)
 */

// Re-export and run the main server
import "../index";
