package keygen

import (
	"gitlab.com/thorchain/tss/go-tss/blame"
	"gitlab.com/thorchain/tss/go-tss/common"
)

// Response keygen response
type Response struct {
	PubKey      string        `json:"pub_key"`
	PoolAddress string        `json:"pool_address"`
	Status      common.Status `json:"status"`
	Blame       blame.Blame   `json:"blame"`
}

// NewResponse create a new instance of keygen.Response
func NewResponse(pk, addr string, status common.Status, blame blame.Blame) Response {
	return Response{
		PubKey:      pk,
		PoolAddress: addr,
		Status:      status,
		Blame:       blame,
	}
}
