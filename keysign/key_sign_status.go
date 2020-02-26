package keysign

import (
	"gitlab.com/thorchain/tss/go-tss/common"
)

// KeySignReq request to sign a message
type KeySignReq struct {
	PoolPubKey    string   `json:"pool_pub_key"`    // pub key of the pool that we would like to send this message from
	Message       string   `json:"message"`         // base64 encoded message to be signed
	SignersPubKey []string `json:"signer_pub_keys"` // all the signers that involved in this round's signing
}

// KeySignResp key sign response
type KeySignResp struct {
	R      string        `json:"r"`
	S      string        `json:"s"`
	Status common.Status `json:"status"`
	Blame  common.Blame  `json:"blame"`
}

func NewKeySignResp(r, s string, status common.Status, blame common.Blame) KeySignResp {
	return KeySignResp{
		R:      r,
		S:      s,
		Status: status,
		Blame:  blame,
	}
}
