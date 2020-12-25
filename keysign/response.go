package keysign

import (
	"gitlab.com/thorchain/tss/go-tss/blame"
	"gitlab.com/thorchain/tss/go-tss/common"
)

// Response key sign response
type Response struct {
	R          string        `json:"r"`
	S          string        `json:"s"`
	RecoveryID string        `json:"recovery_id"`
	Status     common.Status `json:"status"`
	Blame      blame.Blame   `json:"blame"`
}

func NewResponse(r, s, recoveryID string, status common.Status, blame blame.Blame) Response {
	return Response{
		R:          r,
		S:          s,
		RecoveryID: recoveryID,
		Status:     status,
		Blame:      blame,
	}
}
