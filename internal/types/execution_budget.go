package types

type ExecutionBudget struct {
	ID       string `json:"id"`
	Project  string `json:"project"`
	Timezone string `json:"timezone"`
	Unit     string `json:"unit"`
	Limit    int64  `json:"limit"`
	Revision int    `json:"revision"`
}
type BudgetReservation struct {
	ID       string `json:"id"`
	BudgetID string `json:"budget_id"`
	ParentID string `json:"parent_id,omitempty"`
	Window   string `json:"window"`
	Units    int64  `json:"units"`
	State    string `json:"state"`
}
