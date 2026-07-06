package tui

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

// ============================================================================
// Goal Config Modal (Phase 2)
//
// A self-contained Modal for editing a goal automation's configuration. It is
// NOT wired into tui.go key handlers here — that is Phase 3. This component
// owns its own field list, navigation, inline text editing, dropdown cycling,
// multi-select status tracking, save command, and last-reconcile audit display.
// ============================================================================

// Goal-specific pseudo-fields for the multi-select status sub-lists. These are
// modal-local field identifiers (not part of the Phase-1 metadata registry),
// so they render and behave as multi-select status pickers.
const (
	FieldGoalCompleteStatuses MetadataField = "goal_complete_statuses"
	FieldGoalBlockedStatuses  MetadataField = "goal_blocked_statuses"
)

// goalConfigSavedMsg is emitted after a save attempt (apiClient.UpdateGoal)
// completes. Phase 3 wires this into the TUI to refresh and report status.
type goalConfigSavedMsg struct {
	goalID  string
	summary *types.GoalSummary
	err     error
	created bool // true when a new goal was created (vs updated)
}

// goalAuditLoadedMsg is emitted after the reconcile audit history loads.
type goalAuditLoadedMsg struct {
	goalID string
	audit  []types.GoalReconcileAudit
	err    error
}

// goalStatusFieldKind distinguishes the two multi-select status fields.
type goalStatusFieldKind int

const (
	goalStatusKindNone goalStatusFieldKind = iota
	goalStatusKindComplete
	goalStatusKindBlocked
)

// GoalConfigModal implements Modal for editing goal automation config.
type GoalConfigModal struct {
	goalID    string
	apiClient *runner.APIClient

	// fields is the ordered, vertical list of editable fields.
	fields []MetadataField
	// values holds the current text/dropdown value for each scalar field.
	values map[MetadataField]string

	// focusIndex is the currently focused field row.
	focusIndex int

	// editing is true when an inline text-edit buffer is active.
	editing   bool
	editBuf   string
	editField MetadataField

	// Multi-select status sets (keyed by status string -> selected).
	completeStatuses map[string]bool
	blockedStatuses  map[string]bool
	// subCursor is the highlighted index within the focused multi-select.
	subCursor int

	// actionType preserves the goal's existing action type for the save payload.
	actionType string

	// createMode is true when this modal creates a new goal (POST /goals) rather
	// than editing an existing one (PATCH /goals/{id}).
	createMode bool

	// Audit state.
	audit        []types.GoalReconcileAudit
	loadingAudit bool
}

// NewGoalConfigModal builds a goal config modal seeded from the given goal.
func NewGoalConfigModal(goal types.GoalSummary, apiClient *runner.APIClient) *GoalConfigModal {
	values := map[MetadataField]string{}

	// --- Objective (from Title) ---
	values[FieldGoalObjective] = goal.Title

	// --- Config-derived fields ---
	var criteria, validation, workdir, triggerSource string
	var completeSeed, blockedSeed []string
	if goal.Config != nil {
		criteria = goal.Config.Criteria
		validation = goal.Config.Validation
		workdir = goal.Config.Workdir
		triggerSource = goal.Config.TriggerSource
		completeSeed = goal.Config.CompleteStatuses
		blockedSeed = goal.Config.BlockedStatuses
	}
	values[FieldGoalCriteria] = criteria
	values[FieldGoalValidation] = validation

	// Workdir defaults to cwd when empty.
	if workdir == "" {
		if cwd, err := os.Getwd(); err == nil {
			workdir = cwd
		}
	}
	values[FieldGoalWorkdir] = workdir

	// Trigger source: empty/unknown -> "both".
	values[FieldGoalTriggerSource] = normalizeTriggerSource(triggerSource)

	// --- Action-derived fields ---
	var sessionMode, agent, model string
	if goal.Action != nil {
		sessionMode = goal.Action.SessionMode
		agent = goal.Action.Agent
		model = goal.Action.Model
	}
	if sessionMode == "" {
		sessionMode = "continue"
	}
	values[FieldGoalSessionMode] = sessionMode
	values[FieldAgent] = agent
	values[FieldModel] = model

	// Executor has no field on AutomationAction. Store in modal state only and
	// default to "opencode". NOTE(Phase 3/future): when AutomationAction gains
	// an executor field (or goal config carries it), wire this into the save
	// payload. For now it is intentionally NOT sent in buildUpdateRequest().
	values[FieldGoalExecutor] = "opencode"

	// --- Multi-select status sets ---
	complete := map[string]bool{}
	for _, s := range completeSeed {
		complete[s] = true
	}
	blocked := map[string]bool{}
	for _, s := range blockedSeed {
		blocked[s] = true
	}

	m := &GoalConfigModal{
		goalID:           goal.GoalID,
		apiClient:        apiClient,
		values:           values,
		completeStatuses: complete,
		blockedStatuses:  blocked,
		loadingAudit:     true,
		fields: []MetadataField{
			FieldGoalObjective,
			FieldGoalCriteria,
			FieldGoalValidation,
			FieldGoalWorkdir,
			FieldGoalTriggerSource,
			FieldGoalSessionMode,
			FieldAgent,
			FieldModel,
			FieldGoalExecutor,
			FieldGoalCompleteStatuses,
			FieldGoalBlockedStatuses,
		},
	}

	// Preserve the original action type for the save payload (default later).
	if goal.Action != nil {
		m.actionType = goal.Action.Type
	}

	return m
}

// NewGoalCreateModal builds a goal config modal for creating a brand-new goal.
// It seeds empty values (project defaulted to defaultProject) and switches the
// save path to POST /goals. The goal id is generated from the objective on save.
func NewGoalCreateModal(defaultProject string, apiClient *runner.APIClient) *GoalConfigModal {
	workdir := ""
	if cwd, err := os.Getwd(); err == nil {
		workdir = cwd
	}
	values := map[MetadataField]string{
		FieldGoalObjective:     "",
		FieldGoalProject:       defaultProject,
		FieldGoalFeature:       "",
		FieldGoalCriteria:      "",
		FieldGoalValidation:    "",
		FieldGoalWorkdir:       workdir,
		FieldGoalTriggerSource: normalizeTriggerSource(""),
		FieldGoalSessionMode:   "continue",
		FieldAgent:             "",
		FieldModel:             "",
		FieldGoalExecutor:      "opencode",
	}
	return &GoalConfigModal{
		apiClient:        apiClient,
		values:           values,
		completeStatuses: map[string]bool{},
		blockedStatuses:  map[string]bool{},
		createMode:       true,
		actionType:       "prompt",
		fields: []MetadataField{
			FieldGoalObjective,
			FieldGoalProject,
			FieldGoalFeature,
			FieldGoalCriteria,
			FieldGoalValidation,
			FieldGoalWorkdir,
			FieldGoalTriggerSource,
			FieldGoalSessionMode,
			FieldAgent,
			FieldModel,
			FieldGoalExecutor,
			FieldGoalCompleteStatuses,
			FieldGoalBlockedStatuses,
		},
	}
}

// Init loads the reconcile audit history (skipped in create mode).
func (m *GoalConfigModal) Init() tea.Cmd {
	if m.createMode {
		return nil
	}
	apiClient := m.apiClient
	goalID := m.goalID
	return func() tea.Msg {
		ctx := context.Background()
		audit, err := apiClient.GoalAudit(ctx, goalID, 5)
		return goalAuditLoadedMsg{goalID: goalID, audit: audit, err: err}
	}
}

// Update handles async messages (audit load, save result).
func (m *GoalConfigModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case goalAuditLoadedMsg:
		if msg.goalID == m.goalID {
			m.loadingAudit = false
			if msg.err == nil {
				m.audit = msg.audit
			}
		}
		return m, nil
	}
	return m, nil
}

// View renders the field list, footer hint, and last-reconcile section.
func (m *GoalConfigModal) View() string {
	var b strings.Builder

	focusedStyle := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(ColorDim)
	valueStyle := lipgloss.NewStyle().Foreground(ColorReady)

	for i, field := range m.fields {
		focused := i == m.focusIndex
		label := goalFieldLabel(field)
		value := m.displayValue(field)

		marker := "  "
		labelRendered := dimStyle.Render(label)
		if focused {
			marker = "→ "
			labelRendered = focusedStyle.Render(label)
		}

		// In edit mode, show the live buffer with a cursor.
		valRendered := valueStyle.Render(value)
		if focused && m.editing && field == m.editField {
			valRendered = valueStyle.Render(m.editBuf + "▏")
		}

		b.WriteString(fmt.Sprintf("%s%s: %s", marker, labelRendered, valRendered))
		b.WriteString("\n")

		// Render sub-cursor row for focused multi-select fields.
		if focused && isGoalStatusField(field) {
			b.WriteString(m.renderStatusSubrow(field))
			b.WriteString("\n")
		}
	}

	// Footer hint.
	b.WriteString("\n")
	footerStyle := lipgloss.NewStyle().Foreground(ColorDim).Italic(true)
	b.WriteString(footerStyle.Render("j/k: field  l/h: change  enter: edit  ctrl+s: save  esc: cancel"))
	b.WriteString("\n\n")

	// Last Reconcile section (edit mode only — a new goal has no history).
	if m.createMode {
		return b.String()
	}
	titleStyle := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	b.WriteString(titleStyle.Render("Last Reconcile"))
	b.WriteString("\n")
	if m.loadingAudit {
		b.WriteString(dimStyle.Render("Loading…"))
	} else if len(m.audit) == 0 {
		b.WriteString(dimStyle.Render("No reconcile history"))
	} else {
		last := m.audit[0]
		line := fmt.Sprintf("%s — %s", string(last.Decision), last.Reason)
		b.WriteString(valueStyle.Render(line))
		if last.Timestamp != "" {
			b.WriteString("\n")
			b.WriteString(dimStyle.Render(last.Timestamp))
		}
	}
	b.WriteString("\n")

	return b.String()
}

// statusSubrowColumns controls how many status options render per row in the
// multi-select sub-grid. Keeping a fixed column count produces an aligned grid
// instead of a single line that wraps mid-word.
const statusSubrowColumns = 3

// renderStatusSubrow renders the status options for a focused multi-select
// field as an aligned grid. Selected entries use a filled checkbox, the
// sub-cursor entry is highlighted, and cells are padded to a fixed width so
// columns line up.
func (m *GoalConfigModal) renderStatusSubrow(field MetadataField) string {
	selected := m.statusSet(field)
	cursorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(ColorCyan).Bold(true)
	onStyle := lipgloss.NewStyle().Foreground(ColorReady)
	offStyle := lipgloss.NewStyle().Foreground(ColorDim)

	// Compute the widest "[x] status" cell so every column aligns.
	cellWidth := 0
	for _, s := range types.EntryStatuses {
		if w := len(s) + 4; w > cellWidth { // 4 = len("[x] ")
			cellWidth = w
		}
	}

	const indent = "    "
	var b strings.Builder
	for i, s := range types.EntryStatuses {
		if i > 0 && i%statusSubrowColumns == 0 {
			b.WriteString("\n")
		}
		if i%statusSubrowColumns == 0 {
			b.WriteString(indent)
		}

		box := "[ ]"
		if selected[s] {
			box = "[x]"
		}
		cell := fmt.Sprintf("%s %s", box, s)
		// Pad to the fixed cell width before styling so background highlight
		// covers the whole cell and columns stay aligned.
		if pad := cellWidth - len(cell); pad > 0 {
			cell += strings.Repeat(" ", pad)
		}

		switch {
		case i == m.subCursor:
			b.WriteString(cursorStyle.Render(cell))
		case selected[s]:
			b.WriteString(onStyle.Render(cell))
		default:
			b.WriteString(offStyle.Render(cell))
		}
		b.WriteString(" ")
	}
	return b.String()
}

// displayValue returns the printable value for a field row.
func (m *GoalConfigModal) displayValue(field MetadataField) string {
	if isGoalStatusField(field) {
		sel := m.selectedSlice(field)
		if len(sel) == 0 {
			return "(none)"
		}
		return strings.Join(sel, ", ")
	}
	return m.values[field]
}

// HandleKey processes a key press for the modal.
func (m *GoalConfigModal) HandleKey(key string) (bool, tea.Cmd) {
	// Text-edit mode intercepts most keys.
	if m.editing {
		return m.handleEditKey(key)
	}

	switch key {
	case "j", "down":
		m.focusIndex++
		if m.focusIndex >= len(m.fields) {
			m.focusIndex = 0
		}
		m.subCursor = 0
		return true, nil

	case "k", "up":
		m.focusIndex--
		if m.focusIndex < 0 {
			m.focusIndex = len(m.fields) - 1
		}
		m.subCursor = 0
		return true, nil

	case "ctrl+s":
		return true, m.saveCmd()

	case "esc":
		// Not handled in normal mode -> ModalManager closes.
		return false, nil
	}

	field := m.fields[m.focusIndex]
	ft := goalFieldType(field)

	switch ft {
	case FieldTypeDropdown:
		switch key {
		case "l", "right", "enter", "return":
			m.cycleDropdown(field, +1)
			return true, nil
		case "h", "left":
			m.cycleDropdown(field, -1)
			return true, nil
		}
		return true, nil

	case FieldTypeText:
		switch key {
		case "enter", "return":
			m.editing = true
			m.editField = field
			m.editBuf = m.values[field]
			return true, nil
		}
		return true, nil
	}

	// Multi-select status fields.
	if isGoalStatusField(field) {
		switch key {
		case "l", "right":
			m.subCursor++
			if m.subCursor >= len(types.EntryStatuses) {
				m.subCursor = 0
			}
			return true, nil
		case "h", "left":
			m.subCursor--
			if m.subCursor < 0 {
				m.subCursor = len(types.EntryStatuses) - 1
			}
			return true, nil
		case "space", " ", "enter", "return":
			m.toggleStatus(field, types.EntryStatuses[m.subCursor])
			return true, nil
		}
		return true, nil
	}

	// Consume all other keys while open.
	return true, nil
}

// handleEditKey processes keys while in inline text-edit mode.
func (m *GoalConfigModal) handleEditKey(key string) (bool, tea.Cmd) {
	switch key {
	case "enter", "return":
		// Commit.
		m.values[m.editField] = m.editBuf
		m.editing = false
		m.editBuf = ""
		return true, nil
	case "esc":
		// Exit edit mode WITHOUT committing; consume the key.
		m.editing = false
		m.editBuf = ""
		return true, nil
	case "backspace":
		if len(m.editBuf) > 0 {
			r := []rune(m.editBuf)
			m.editBuf = string(r[:len(r)-1])
		}
		return true, nil
	case "space":
		m.editBuf += " "
		return true, nil
	default:
		// Append printable single-rune keys.
		if len([]rune(key)) == 1 {
			m.editBuf += key
		}
		return true, nil
	}
}

// cycleDropdown advances the dropdown value by dir (+1/-1) with wrap.
func (m *GoalConfigModal) cycleDropdown(field MetadataField, dir int) {
	opts := goalEnumOptions(field)
	if len(opts) == 0 {
		return
	}
	cur := m.values[field]
	idx := 0
	for i, o := range opts {
		if o == cur {
			idx = i
			break
		}
	}
	idx = (idx + dir) % len(opts)
	if idx < 0 {
		idx += len(opts)
	}
	m.values[field] = opts[idx]
}

// toggleStatus flips membership of status in the field's multi-select set.
func (m *GoalConfigModal) toggleStatus(field MetadataField, status string) {
	set := m.statusSet(field)
	if set[status] {
		delete(set, status)
	} else {
		set[status] = true
	}
}

// statusSet returns the underlying selection map for a status field.
func (m *GoalConfigModal) statusSet(field MetadataField) map[string]bool {
	if field == FieldGoalBlockedStatuses {
		return m.blockedStatuses
	}
	return m.completeStatuses
}

// selectedSlice returns the selected statuses for a field in EntryStatuses order.
func (m *GoalConfigModal) selectedSlice(field MetadataField) []string {
	set := m.statusSet(field)
	out := make([]string, 0, len(set))
	for _, s := range types.EntryStatuses {
		if set[s] {
			out = append(out, s)
		}
	}
	return out
}

// CompleteStatuses returns the selected complete statuses (EntryStatuses order).
func (m *GoalConfigModal) CompleteStatuses() []string {
	return m.selectedSlice(FieldGoalCompleteStatuses)
}

// BlockedStatuses returns the selected blocked statuses (EntryStatuses order).
func (m *GoalConfigModal) BlockedStatuses() []string {
	return m.selectedSlice(FieldGoalBlockedStatuses)
}

// buildUpdateRequest builds the UpdateGoalRequest from current modal values.
// It is unit-testable without network access.
func (m *GoalConfigModal) buildUpdateRequest() types.UpdateGoalRequest {
	objective := m.values[FieldGoalObjective]
	criteria := m.values[FieldGoalCriteria]
	validation := m.values[FieldGoalValidation]
	workdir := m.values[FieldGoalWorkdir]
	triggerSource := m.values[FieldGoalTriggerSource]

	complete := m.CompleteStatuses()
	blocked := m.BlockedStatuses()

	actionType := m.actionType
	if actionType == "" {
		actionType = "prompt"
	}

	action := &types.AutomationAction{
		Type:        actionType,
		Agent:       m.values[FieldAgent],
		Model:       m.values[FieldModel],
		SessionMode: m.values[FieldGoalSessionMode],
		// NOTE(Phase 3/future): executor (m.values[FieldGoalExecutor]) is
		// intentionally NOT sent — AutomationAction has no executor field yet.
	}

	return types.UpdateGoalRequest{
		Title:            &objective,
		Criteria:         &criteria,
		Validation:       &validation,
		Workdir:          &workdir,
		TriggerSource:    &triggerSource,
		CompleteStatuses: &complete,
		BlockedStatuses:  &blocked,
		Action:           action,
	}
}

// buildCreateRequest builds a CreateGoalRequest from current modal values.
func (m *GoalConfigModal) buildCreateRequest() types.CreateGoalRequest {
	objective := strings.TrimSpace(m.values[FieldGoalObjective])
	criteria := m.values[FieldGoalCriteria]
	content := criteria
	if strings.TrimSpace(content) == "" {
		content = objective
	}
	actionType := m.actionType
	if actionType == "" {
		actionType = "prompt"
	}
	return types.CreateGoalRequest{
		Project:   strings.TrimSpace(m.values[FieldGoalProject]),
		FeatureID: strings.TrimSpace(m.values[FieldGoalFeature]),
		Title:     objective,
		Content:   content,
		Config: types.GoalConfig{
			ID:               generateGoalID(objective),
			Criteria:         criteria,
			Validation:       m.values[FieldGoalValidation],
			Workdir:          m.values[FieldGoalWorkdir],
			TriggerSource:    m.values[FieldGoalTriggerSource],
			CompleteStatuses: m.CompleteStatuses(),
			BlockedStatuses:  m.BlockedStatuses(),
		},
		Action: types.AutomationAction{
			Type:        actionType,
			Agent:       m.values[FieldAgent],
			Model:       m.values[FieldModel],
			SessionMode: m.values[FieldGoalSessionMode],
		},
	}
}

// saveCmd builds a command that persists the goal and emits a typed msg.
// In create mode it POSTs a new goal; otherwise it PATCHes the existing one.
func (m *GoalConfigModal) saveCmd() tea.Cmd {
	apiClient := m.apiClient

	if m.createMode {
		// Guard required fields client-side for a clear message.
		if strings.TrimSpace(m.values[FieldGoalObjective]) == "" ||
			strings.TrimSpace(m.values[FieldGoalProject]) == "" {
			return func() tea.Msg {
				return goalConfigSavedMsg{created: true, err: fmt.Errorf("objective and project are required")}
			}
		}
		req := m.buildCreateRequest()
		return func() tea.Msg {
			ctx := context.Background()
			summary, err := apiClient.CreateGoal(ctx, req)
			goalID := ""
			if summary != nil {
				goalID = summary.GoalID
			}
			return goalConfigSavedMsg{goalID: goalID, summary: summary, err: err, created: true}
		}
	}

	goalID := m.goalID
	req := m.buildUpdateRequest()
	return func() tea.Msg {
		ctx := context.Background()
		summary, err := apiClient.UpdateGoal(ctx, goalID, req)
		return goalConfigSavedMsg{goalID: goalID, summary: summary, err: err}
	}
}

// generateGoalID derives a goal id from the objective (slug + short suffix).
func generateGoalID(objective string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(objective) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	if slug == "" {
		slug = "goal"
	}
	buf := make([]byte, 2)
	if _, err := rand.Read(buf); err == nil {
		return fmt.Sprintf("%s-%02x%02x", slug, buf[0], buf[1])
	}
	return slug
}

// Title implements Modal.
func (m *GoalConfigModal) Title() string {
	if m.createMode {
		return "New Goal"
	}
	return "Configure Goal"
}

// Width implements Modal.
func (m *GoalConfigModal) Width() int { return 64 }

// Height implements Modal.
func (m *GoalConfigModal) Height() int {
	lines := len(m.fields) // one row per field
	lines += 2             // a focused multi-select expands a subrow (reserve a couple)
	lines += 2             // blank + footer
	lines += 4             // blank + "Last Reconcile" title + audit line + timestamp
	return lines
}

// ============================================================================
// Helpers
// ============================================================================

func normalizeTriggerSource(s string) string {
	switch s {
	case types.GoalTriggerSourceTask, types.GoalTriggerSourceFeature, types.GoalTriggerSourceBoth:
		return s
	default:
		return types.GoalTriggerSourceBoth
	}
}

func isGoalStatusField(field MetadataField) bool {
	return field == FieldGoalCompleteStatuses || field == FieldGoalBlockedStatuses
}

// goalFieldLabel returns a display label, including labels for the modal-local
// multi-select status fields not present in the metadata registry.
func goalFieldLabel(field MetadataField) string {
	switch field {
	case FieldGoalCompleteStatuses:
		return "Complete Statuses"
	case FieldGoalBlockedStatuses:
		return "Blocked Statuses"
	default:
		return getFieldLabel(field)
	}
}

// goalFieldType returns the field type, treating the modal-local status fields
// as a custom (non-registry) multi-select that HandleKey routes manually.
func goalFieldType(field MetadataField) FieldType {
	if isGoalStatusField(field) {
		return FieldTypeMultiFilterDropdown
	}
	return getFieldType(field)
}

func goalEnumOptions(field MetadataField) []string {
	return getEnumOptions(field)
}
