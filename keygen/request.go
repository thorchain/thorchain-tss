package keygen

// Request request to do keygen
type Request struct {
	Keys []string `json:"keys"`
	Algo string   `json:"algo"`
}

// NewRequest creeate a new instance of keygen.Request
func NewRequest(keys []string, algo string) Request {
	return Request{
		Keys: keys,
		Algo: algo,
	}
}
