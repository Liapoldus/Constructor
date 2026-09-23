package application

import "github.com/Liapoldus/Constructor/internal/domain"

// DeploymentOutcomeUncertain means the Gateway request may have committed but
// Constructor could not confirm its result. The persisted deployment remains
// applying and is reserved for idempotent recovery.
type DeploymentOutcomeUncertain struct {
	Deployment domain.Deployment
	cause      error
}

func (e *DeploymentOutcomeUncertain) Error() string {
	return "Gateway operation outcome is not yet confirmed"
}

func (e *DeploymentOutcomeUncertain) Unwrap() error { return e.cause }
