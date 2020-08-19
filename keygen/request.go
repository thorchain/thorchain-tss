package keygen

// Request request to do keygen
type Request struct {
	Keys        []string `json:"keys"`
	BlockHeight int64    `json:"block_height"`
	Version     string   `json:"tss_version"`
	Algo        string   `json:"algo"`
}

// NewRequest creeate a new instance of keygen.Request
func NewRequest(keys []string, blockHeight int64, version, algo string) Request {
	return Request{
		Keys:        keys,
		BlockHeight: blockHeight,
		Version:     version,
		Algo:        algo,
	}
}
