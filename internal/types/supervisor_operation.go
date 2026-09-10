package types

import "time"

type SupervisorOperation struct {
	ID        string    `json:"id"`
	Operation string    `json:"operation"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Detail    string    `json:"detail"`
}
