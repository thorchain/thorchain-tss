package keysign

// Request request to sign a message
type Request struct {
	PoolPubKey    string   `json:"pool_pub_key"` // pub key of the pool that we would like to send this message from
	Message       string   `json:"message"`      // base64 encoded message to be signed
	SignerPubKeys []string `json:"signer_pub_keys"`
	BlockHeight   int64    `json:"block_height"`
	Version       string   `json:"tss_version"`
	Algo          string   `json:"algo"`
}

func NewRequest(pk, msg, algo string, blockHeight int64, signers []string, version string) Request {
	return Request{
		PoolPubKey:    pk,
		Message:       msg,
		SignerPubKeys: signers,
		BlockHeight:   blockHeight,
		Version:       version,
		Algo:          algo,
	}
}
