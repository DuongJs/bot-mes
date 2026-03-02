package messaging

import "errors"

var (
	ErrTransportUnavailable = errors.New("messaging transport unavailable")
	ErrMessageNotFound      = errors.New("message not found")
	ErrEditNotConfirmed     = errors.New("edit not confirmed")
)
