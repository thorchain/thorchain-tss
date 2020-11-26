package keysign

import (
	"gitlab.com/thorchain/tss/go-tss/blame"
	"gitlab.com/thorchain/tss/go-tss/common"
)

// signature
type Signature struct {
	Msg string `json:"signed_msg"`
	R   string `json:"r"`
	S   string `json:"s"`
}

// Response key sign response
type Response struct {
	Signatures []Signature   `json:"signatures"`
	Status     common.Status `json:"status"`
	Blame      blame.Blame   `json:"blame"`
}

func NewSignature(msg, r, s string) Signature {
	return Signature{
		Msg: msg,
		R:   r,
		S:   s,
	}
}

func NewResponse(signatures []Signature, status common.Status, blame blame.Blame) Response {
	return Response{
		Signatures: signatures,
		Status:     status,
		Blame:      blame,
	}
}
