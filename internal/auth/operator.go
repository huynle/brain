package auth

import "errors"

// ErrOperatorRequired means no authenticated deployment-operator authority was
// supplied. An API admin scope or a local tenant binding is not this authority.
var ErrOperatorRequired = errors.New("authenticated deployment operator required")

// DeploymentOperator is an in-process capability minted by the configured
// operator password verifier or the audited local OS database-owner seam.
// The zero value denies access; it has no serialized
// representation or caller-settable fields. Keep it within trusted composition,
// for the lifetime of the authenticated operation; it is not a session token and
// has no expiry/revocation machinery.
type DeploymentOperator struct{ authenticated bool }

// Valid reports whether deployment-operator authentication succeeded.
func (c DeploymentOperator) Valid() bool { return c.authenticated }

// AuthenticateOperator uses the existing constant-time username/bcrypt password
// verifier. There is deliberately no constructor from scopes, tenant IDs or a
// caller-provided boolean/verifier interface.
func (v *Verifier) AuthenticateOperator(username, password string) (DeploymentOperator, error) {
	if !v.Verify(username, password) {
		return DeploymentOperator{}, ErrOperatorRequired
	}
	return DeploymentOperator{authenticated: true}, nil
}
