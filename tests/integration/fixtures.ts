/**
 * Integration Test Fixtures
 *
 * Reusable test data for integration tests. Provides a comprehensive set of
 * NoteRow objects covering multiple projects, types, statuses, priorities,
 * and inter-linked entries.
 *
 * Use `seedStorage(storage)` to insert all fixtures into a StorageLayer.
 * Use `seedBrainDir(brainDir)` to write all fixtures as markdown files on disk.
 */

import { makeTestNote, writeTestMarkdownFile, type StorageLayer, type NoteRow } from "./helpers";

// =============================================================================
// Fixture Link Type
// =============================================================================

export interface FixtureLink {
  source_path: string;
  target_path: string;
  title: string;
}

// =============================================================================
// FIXTURE_NOTES — 24 notes across 3 projects + global
// =============================================================================

export const FIXTURE_NOTES: Omit<NoteRow, "id" | "indexed_at">[] = [
  // =========================================================================
  // Project Alpha — 8 notes
  // =========================================================================
  makeTestNote({
    path: "projects/alpha/task/alph0001.md",
    short_id: "alph0001",
    title: "Implement authentication module",
    lead: "Build JWT-based auth for the API",
    body: "Implement JWT authentication with refresh tokens.\n\nSee [Auth Design](projects/alpha/decision/alph0005.md) for architecture decisions.",
    raw_content: "---\ntitle: Implement authentication module\n---\nImplement JWT authentication with refresh tokens.",
    word_count: 12,
    checksum: "fix_alph0001",
    metadata: JSON.stringify({ title: "Implement authentication module" }),
    type: "task",
    status: "active",
    priority: "high",
    project_id: "alpha",
    feature_id: "auth",
    created: "2024-01-01T00:00:00Z",
    modified: "2024-01-15T00:00:00Z",
  }),
  makeTestNote({
    path: "projects/alpha/task/alph0002.md",
    short_id: "alph0002",
    title: "Write API integration tests",
    lead: "Create comprehensive test suite",
    body: "Write integration tests for all API endpoints.\n\nDepends on [Auth Module](projects/alpha/task/alph0001.md).",
    raw_content: "---\ntitle: Write API integration tests\n---\nWrite integration tests for all API endpoints.",
    word_count: 10,
    checksum: "fix_alph0002",
    metadata: JSON.stringify({ title: "Write API integration tests" }),
    type: "task",
    status: "in_progress",
    priority: "high",
    project_id: "alpha",
    feature_id: "testing",
    created: "2024-01-02T00:00:00Z",
    modified: "2024-01-16T00:00:00Z",
  }),
  makeTestNote({
    path: "projects/alpha/plan/alph0003.md",
    short_id: "alph0003",
    title: "Alpha project roadmap Q1",
    lead: "Quarterly planning for alpha",
    body: "Q1 goals: authentication, testing, deployment pipeline.\n\nRelated: [Auth Task](projects/alpha/task/alph0001.md) and [Test Task](projects/alpha/task/alph0002.md).",
    raw_content: "---\ntitle: Alpha project roadmap Q1\n---\nQ1 goals: authentication, testing, deployment pipeline.",
    word_count: 10,
    checksum: "fix_alph0003",
    metadata: JSON.stringify({ title: "Alpha project roadmap Q1" }),
    type: "plan",
    status: "active",
    priority: "medium",
    project_id: "alpha",
    feature_id: null,
    created: "2024-01-03T00:00:00Z",
    modified: "2024-01-17T00:00:00Z",
  }),
  makeTestNote({
    path: "projects/alpha/exploration/alph0004.md",
    short_id: "alph0004",
    title: "Evaluate OAuth2 providers",
    lead: "Research OAuth2 options",
    body: "Comparing Auth0, Keycloak, and custom OAuth2 implementation.\n\nFindings will inform [Auth Design](projects/alpha/decision/alph0005.md).",
    raw_content: "---\ntitle: Evaluate OAuth2 providers\n---\nComparing Auth0, Keycloak, and custom OAuth2 implementation.",
    word_count: 9,
    checksum: "fix_alph0004",
    metadata: JSON.stringify({ title: "Evaluate OAuth2 providers" }),
    type: "exploration",
    status: "completed",
    priority: "medium",
    project_id: "alpha",
    feature_id: "auth",
    created: "2024-01-04T00:00:00Z",
    modified: "2024-01-10T00:00:00Z",
  }),
  makeTestNote({
    path: "projects/alpha/decision/alph0005.md",
    short_id: "alph0005",
    title: "ADR: Use JWT for API authentication",
    lead: "Architecture decision record for auth",
    body: "Decision: Use JWT with RS256 signing.\n\nContext: See [OAuth2 Evaluation](projects/alpha/exploration/alph0004.md).",
    raw_content: "---\ntitle: ADR: Use JWT for API authentication\n---\nDecision: Use JWT with RS256 signing.",
    word_count: 8,
    checksum: "fix_alph0005",
    metadata: JSON.stringify({ title: "ADR: Use JWT for API authentication" }),
    type: "decision",
    status: "active",
    priority: "high",
    project_id: "alpha",
    feature_id: "auth",
    created: "2024-01-05T00:00:00Z",
    modified: "2024-01-12T00:00:00Z",
  }),
  makeTestNote({
    path: "projects/alpha/idea/alph0006.md",
    short_id: "alph0006",
    title: "Consider GraphQL gateway",
    lead: "Idea for API layer",
    body: "Could add a GraphQL layer on top of REST endpoints for flexible querying.",
    raw_content: "---\ntitle: Consider GraphQL gateway\n---\nCould add a GraphQL layer on top of REST endpoints.",
    word_count: 14,
    checksum: "fix_alph0006",
    metadata: JSON.stringify({ title: "Consider GraphQL gateway" }),
    type: "idea",
    status: "draft",
    priority: "low",
    project_id: "alpha",
    feature_id: null,
    created: "2024-01-06T00:00:00Z",
    modified: "2024-01-06T00:00:00Z",
  }),
  makeTestNote({
    path: "projects/alpha/learning/alph0007.md",
    short_id: "alph0007",
    title: "Bun SQLite performance characteristics",
    lead: "Performance notes on bun:sqlite",
    body: "bun:sqlite is significantly faster than better-sqlite3 for most operations. WAL mode recommended for concurrent reads.",
    raw_content: "---\ntitle: Bun SQLite performance characteristics\n---\nbun:sqlite is significantly faster.",
    word_count: 16,
    checksum: "fix_alph0007",
    metadata: JSON.stringify({ title: "Bun SQLite performance characteristics" }),
    type: "learning",
    status: "active",
    priority: "low",
    project_id: "alpha",
    feature_id: null,
    created: "2024-01-07T00:00:00Z",
    modified: "2024-01-07T00:00:00Z",
  }),
  makeTestNote({
    path: "projects/alpha/summary/alph0008.md",
    short_id: "alph0008",
    title: "Sprint 1 retrospective",
    lead: "Summary of sprint 1 outcomes",
    body: "Completed auth module design. Tests in progress. Deployment pipeline blocked on CI setup.",
    raw_content: "---\ntitle: Sprint 1 retrospective\n---\nCompleted auth module design.",
    word_count: 13,
    checksum: "fix_alph0008",
    metadata: JSON.stringify({ title: "Sprint 1 retrospective" }),
    type: "summary",
    status: "completed",
    priority: "low",
    project_id: "alpha",
    feature_id: null,
    created: "2024-01-08T00:00:00Z",
    modified: "2024-01-18T00:00:00Z",
  }),

  // =========================================================================
  // Project Beta — 8 notes
  // =========================================================================
  makeTestNote({
    path: "projects/beta/task/beta0001.md",
    short_id: "beta0001",
    title: "Set up CI/CD pipeline",
    lead: "Configure GitHub Actions",
    body: "Set up GitHub Actions for automated testing and deployment.",
    raw_content: "---\ntitle: Set up CI/CD pipeline\n---\nSet up GitHub Actions.",
    word_count: 9,
    checksum: "fix_beta0001",
    metadata: JSON.stringify({ title: "Set up CI/CD pipeline" }),
    type: "task",
    status: "completed",
    priority: "high",
    project_id: "beta",
    feature_id: "devops",
    created: "2024-02-01T00:00:00Z",
    modified: "2024-02-10T00:00:00Z",
  }),
  makeTestNote({
    path: "projects/beta/task/beta0002.md",
    short_id: "beta0002",
    title: "Migrate database to PostgreSQL",
    lead: "Database migration plan",
    body: "Migrate from SQLite to PostgreSQL for production.\n\nSee [Migration Plan](projects/beta/plan/beta0003.md).",
    raw_content: "---\ntitle: Migrate database to PostgreSQL\n---\nMigrate from SQLite to PostgreSQL.",
    word_count: 8,
    checksum: "fix_beta0002",
    metadata: JSON.stringify({ title: "Migrate database to PostgreSQL" }),
    type: "task",
    status: "blocked",
    priority: "high",
    project_id: "beta",
    feature_id: "database",
    created: "2024-02-02T00:00:00Z",
    modified: "2024-02-15T00:00:00Z",
  }),
  makeTestNote({
    path: "projects/beta/plan/beta0003.md",
    short_id: "beta0003",
    title: "Database migration strategy",
    lead: "Step-by-step migration plan",
    body: "Phase 1: Schema migration. Phase 2: Data migration. Phase 3: Cutover.",
    raw_content: "---\ntitle: Database migration strategy\n---\nPhase 1: Schema migration.",
    word_count: 10,
    checksum: "fix_beta0003",
    metadata: JSON.stringify({ title: "Database migration strategy" }),
    type: "plan",
    status: "active",
    priority: "medium",
    project_id: "beta",
    feature_id: "database",
    created: "2024-02-03T00:00:00Z",
    modified: "2024-02-12T00:00:00Z",
  }),
  makeTestNote({
    path: "projects/beta/exploration/beta0004.md",
    short_id: "beta0004",
    title: "Evaluate container orchestration options",
    lead: "Kubernetes vs Docker Swarm",
    body: "Comparing Kubernetes, Docker Swarm, and Nomad for container orchestration.",
    raw_content: "---\ntitle: Evaluate container orchestration options\n---\nComparing Kubernetes.",
    word_count: 9,
    checksum: "fix_beta0004",
    metadata: JSON.stringify({ title: "Evaluate container orchestration options" }),
    type: "exploration",
    status: "active",
    priority: "medium",
    project_id: "beta",
    feature_id: "devops",
    created: "2024-02-04T00:00:00Z",
    modified: "2024-02-08T00:00:00Z",
  }),
  makeTestNote({
    path: "projects/beta/idea/beta0005.md",
    short_id: "beta0005",
    title: "Implement feature flags service",
    lead: "Feature flag management",
    body: "Build a lightweight feature flags service for gradual rollouts.",
    raw_content: "---\ntitle: Implement feature flags service\n---\nBuild a lightweight feature flags service.",
    word_count: 10,
    checksum: "fix_beta0005",
    metadata: JSON.stringify({ title: "Implement feature flags service" }),
    type: "idea",
    status: "draft",
    priority: "low",
    project_id: "beta",
    feature_id: null,
    created: "2024-02-05T00:00:00Z",
    modified: "2024-02-05T00:00:00Z",
  }),
  makeTestNote({
    path: "projects/beta/decision/beta0006.md",
    short_id: "beta0006",
    title: "ADR: Use Kubernetes for orchestration",
    lead: "Container orchestration decision",
    body: "Decision: Use Kubernetes with Helm charts.\n\nBased on [Orchestration Evaluation](projects/beta/exploration/beta0004.md).",
    raw_content: "---\ntitle: ADR: Use Kubernetes for orchestration\n---\nDecision: Use Kubernetes.",
    word_count: 7,
    checksum: "fix_beta0006",
    metadata: JSON.stringify({ title: "ADR: Use Kubernetes for orchestration" }),
    type: "decision",
    status: "active",
    priority: "high",
    project_id: "beta",
    feature_id: "devops",
    created: "2024-02-06T00:00:00Z",
    modified: "2024-02-14T00:00:00Z",
  }),
  makeTestNote({
    path: "projects/beta/learning/beta0007.md",
    short_id: "beta0007",
    title: "Kubernetes networking gotchas",
    lead: "Lessons learned from K8s setup",
    body: "Service mesh adds complexity. Start with simple ClusterIP services before adding Istio.",
    raw_content: "---\ntitle: Kubernetes networking gotchas\n---\nService mesh adds complexity.",
    word_count: 13,
    checksum: "fix_beta0007",
    metadata: JSON.stringify({ title: "Kubernetes networking gotchas" }),
    type: "learning",
    status: "active",
    priority: "medium",
    project_id: "beta",
    feature_id: "devops",
    created: "2024-02-07T00:00:00Z",
    modified: "2024-02-07T00:00:00Z",
  }),
  makeTestNote({
    path: "projects/beta/summary/beta0008.md",
    short_id: "beta0008",
    title: "Q1 infrastructure review",
    lead: "Infrastructure status summary",
    body: "CI/CD complete. Database migration blocked. Container orchestration decided.",
    raw_content: "---\ntitle: Q1 infrastructure review\n---\nCI/CD complete.",
    word_count: 9,
    checksum: "fix_beta0008",
    metadata: JSON.stringify({ title: "Q1 infrastructure review" }),
    type: "summary",
    status: "completed",
    priority: "low",
    project_id: "beta",
    feature_id: null,
    created: "2024-02-08T00:00:00Z",
    modified: "2024-02-16T00:00:00Z",
  }),

  // =========================================================================
  // Project Gamma — 5 notes
  // =========================================================================
  makeTestNote({
    path: "projects/gamma/task/gamm0001.md",
    short_id: "gamm0001",
    title: "Design user onboarding flow",
    lead: "UX design for onboarding",
    body: "Create wireframes and user flow for the onboarding experience.",
    raw_content: "---\ntitle: Design user onboarding flow\n---\nCreate wireframes.",
    word_count: 10,
    checksum: "fix_gamm0001",
    metadata: JSON.stringify({ title: "Design user onboarding flow" }),
    type: "task",
    status: "active",
    priority: "high",
    project_id: "gamma",
    feature_id: "onboarding",
    created: "2024-03-01T00:00:00Z",
    modified: "2024-03-10T00:00:00Z",
  }),
  makeTestNote({
    path: "projects/gamma/task/gamm0002.md",
    short_id: "gamm0002",
    title: "Implement email notification service",
    lead: "Email service for notifications",
    body: "Build transactional email service using SendGrid API.",
    raw_content: "---\ntitle: Implement email notification service\n---\nBuild transactional email service.",
    word_count: 7,
    checksum: "fix_gamm0002",
    metadata: JSON.stringify({ title: "Implement email notification service" }),
    type: "task",
    status: "draft",
    priority: "medium",
    project_id: "gamma",
    feature_id: "notifications",
    created: "2024-03-02T00:00:00Z",
    modified: "2024-03-02T00:00:00Z",
  }),
  makeTestNote({
    path: "projects/gamma/plan/gamm0003.md",
    short_id: "gamm0003",
    title: "Gamma launch checklist",
    lead: "Pre-launch requirements",
    body: "Checklist: onboarding, notifications, monitoring, documentation.",
    raw_content: "---\ntitle: Gamma launch checklist\n---\nChecklist: onboarding, notifications.",
    word_count: 5,
    checksum: "fix_gamm0003",
    metadata: JSON.stringify({ title: "Gamma launch checklist" }),
    type: "plan",
    status: "in_progress",
    priority: "high",
    project_id: "gamma",
    feature_id: null,
    created: "2024-03-03T00:00:00Z",
    modified: "2024-03-15T00:00:00Z",
  }),
  makeTestNote({
    path: "projects/gamma/exploration/gamm0004.md",
    short_id: "gamm0004",
    title: "研究: Internationalization approaches",
    lead: "i18n research with unicode title",
    body: "Evaluating i18next, FormatJS, and custom solutions for 多言語サポート (multilingual support).",
    raw_content: "---\ntitle: '研究: Internationalization approaches'\n---\nEvaluating i18next.",
    word_count: 9,
    checksum: "fix_gamm0004",
    metadata: JSON.stringify({ title: "研究: Internationalization approaches" }),
    type: "exploration",
    status: "active",
    priority: "medium",
    project_id: "gamma",
    feature_id: "i18n",
    created: "2024-03-04T00:00:00Z",
    modified: "2024-03-08T00:00:00Z",
  }),
  makeTestNote({
    path: "projects/gamma/idea/gamm0005.md",
    short_id: "gamm0005",
    title: "Idée: AI-powered content suggestions",
    lead: "AI content idea with unicode",
    body: "Use LLM to suggest content improvements. Könnte die Benutzererfahrung verbessern.",
    raw_content: "---\ntitle: 'Idée: AI-powered content suggestions'\n---\nUse LLM to suggest content improvements.",
    word_count: 10,
    checksum: "fix_gamm0005",
    metadata: JSON.stringify({ title: "Idée: AI-powered content suggestions" }),
    type: "idea",
    status: "draft",
    priority: "low",
    project_id: "gamma",
    feature_id: null,
    created: "2024-03-05T00:00:00Z",
    modified: "2024-03-05T00:00:00Z",
  }),

  // =========================================================================
  // Global (no project) — 3 notes
  // =========================================================================
  makeTestNote({
    path: "global/learning/glob0001.md",
    short_id: "glob0001",
    title: "TypeScript strict mode best practices",
    lead: "TS strict mode tips",
    body: "Always enable strict mode. Use unknown instead of any. Prefer type guards over assertions.",
    raw_content: "---\ntitle: TypeScript strict mode best practices\n---\nAlways enable strict mode.",
    word_count: 15,
    checksum: "fix_glob0001",
    metadata: JSON.stringify({ title: "TypeScript strict mode best practices" }),
    type: "learning",
    status: "active",
    priority: "medium",
    project_id: null,
    feature_id: null,
    created: "2024-01-15T00:00:00Z",
    modified: "2024-01-15T00:00:00Z",
  }),
  makeTestNote({
    path: "global/pattern/glob0002.md",
    short_id: "glob0002",
    title: "Repository pattern for data access",
    lead: "Data access pattern",
    body: "Wrap database operations in a repository class. Enables testing with in-memory implementations.",
    raw_content: "---\ntitle: Repository pattern for data access\n---\nWrap database operations.",
    word_count: 13,
    checksum: "fix_glob0002",
    metadata: JSON.stringify({ title: "Repository pattern for data access" }),
    type: "learning",
    status: "active",
    priority: "low",
    project_id: null,
    feature_id: null,
    created: "2024-02-15T00:00:00Z",
    modified: "2024-02-15T00:00:00Z",
  }),
  makeTestNote({
    path: "global/decision/glob0003.md",
    short_id: "glob0003",
    title: "ADR: Adopt Bun as runtime",
    lead: "Runtime decision",
    body: "Decision: Use Bun instead of Node.js for better performance and built-in SQLite support.",
    raw_content: "---\ntitle: ADR: Adopt Bun as runtime\n---\nDecision: Use Bun instead of Node.js.",
    word_count: 14,
    checksum: "fix_glob0003",
    metadata: JSON.stringify({ title: "ADR: Adopt Bun as runtime" }),
    type: "decision",
    status: "active",
    priority: "high",
    project_id: null,
    feature_id: null,
    created: "2024-01-01T00:00:00Z",
    modified: "2024-01-01T00:00:00Z",
  }),
];

// =============================================================================
// FIXTURE_LINKS — Link relationships between fixture notes
// =============================================================================

export const FIXTURE_LINKS: FixtureLink[] = [
  // Alpha: plan links to tasks
  {
    source_path: "projects/alpha/plan/alph0003.md",
    target_path: "projects/alpha/task/alph0001.md",
    title: "Auth Module",
  },
  {
    source_path: "projects/alpha/plan/alph0003.md",
    target_path: "projects/alpha/task/alph0002.md",
    title: "Test Task",
  },
  // Alpha: task links to decision
  {
    source_path: "projects/alpha/task/alph0001.md",
    target_path: "projects/alpha/decision/alph0005.md",
    title: "Auth Design",
  },
  // Alpha: task depends on another task
  {
    source_path: "projects/alpha/task/alph0002.md",
    target_path: "projects/alpha/task/alph0001.md",
    title: "Auth Module",
  },
  // Alpha: exploration informs decision
  {
    source_path: "projects/alpha/exploration/alph0004.md",
    target_path: "projects/alpha/decision/alph0005.md",
    title: "Auth Design",
  },
  // Alpha: decision references exploration
  {
    source_path: "projects/alpha/decision/alph0005.md",
    target_path: "projects/alpha/exploration/alph0004.md",
    title: "OAuth2 Evaluation",
  },
  // Beta: task links to plan
  {
    source_path: "projects/beta/task/beta0002.md",
    target_path: "projects/beta/plan/beta0003.md",
    title: "Migration Plan",
  },
  // Beta: decision references exploration
  {
    source_path: "projects/beta/decision/beta0006.md",
    target_path: "projects/beta/exploration/beta0004.md",
    title: "Orchestration Evaluation",
  },
  // Cross-project: gamma references alpha learning
  {
    source_path: "projects/gamma/task/gamm0001.md",
    target_path: "projects/alpha/learning/alph0007.md",
    title: "SQLite Performance",
  },
  // Global: decision references global learning
  {
    source_path: "global/decision/glob0003.md",
    target_path: "global/learning/glob0001.md",
    title: "TypeScript Best Practices",
  },
];

// =============================================================================
// Seed Functions
// =============================================================================

/**
 * Insert all fixture notes and links into a StorageLayer.
 * Notes are inserted first, then links are set up with resolved target_ids.
 */
export function seedStorage(storage: StorageLayer): void {
  // Insert all notes
  for (const note of FIXTURE_NOTES) {
    storage.insertNote(note);
  }

  // Group links by source_path for setLinks
  const linksBySource = new Map<string, Array<{
    target_path: string;
    target_id: null;
    title: string;
    href: string;
    type: string;
    snippet: string;
  }>>();

  for (const link of FIXTURE_LINKS) {
    if (!linksBySource.has(link.source_path)) {
      linksBySource.set(link.source_path, []);
    }
    linksBySource.get(link.source_path)!.push({
      target_path: link.target_path,
      target_id: null,
      title: link.title,
      href: link.target_path,
      type: "markdown",
      snippet: "",
    });
  }

  // Set links for each source note
  for (const [sourcePath, links] of linksBySource) {
    storage.setLinks(sourcePath, links);
  }
}

/**
 * Write all fixture notes as markdown files in a brain directory.
 * Creates proper directory structure and YAML frontmatter.
 */
export function seedBrainDir(brainDir: string): void {
  for (const note of FIXTURE_NOTES) {
    // Parse metadata to extract frontmatter fields
    const metadata = JSON.parse(note.metadata) as Record<string, unknown>;

    const frontmatter: Record<string, unknown> = {
      title: note.title,
    };

    if (note.type) frontmatter.type = note.type;
    if (note.status) frontmatter.status = note.status;
    if (note.priority) frontmatter.priority = note.priority;
    if (note.project_id) frontmatter.projectId = note.project_id;
    if (note.feature_id) frontmatter.feature_id = note.feature_id;
    if (note.created) frontmatter.created = note.created;
    if (note.modified) frontmatter.modified = note.modified;

    writeTestMarkdownFile(brainDir, note.path, frontmatter, note.body);
  }
}
