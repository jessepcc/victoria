package domain

import "errors"

var (
	ErrNotFound             = errors.New("not found")
	ErrTenantMismatch       = errors.New("tenant mismatch")
	ErrDuplicate            = errors.New("duplicate")
	ErrInvalidInput         = errors.New("invalid input")
	ErrSandboxContamination = errors.New("sandbox contamination")
	ErrApprovalRequired     = errors.New("approval required")
	ErrSandboxMode          = errors.New("sandbox mode")
	ErrCapabilityDenied     = errors.New("capability denied")
	ErrSecurityViolation    = errors.New("security violation")
	ErrExpired              = errors.New("expired")
)
