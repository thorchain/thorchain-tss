package blame

import (
	"errors"
	"sync"

	btss "github.com/binance-chain/tss-lib/tss"
)

const (
	HashCheckFail = "hash check failed"
	TssTimeout    = "Tss timeout"
	TssSyncFail   = "signers fail to sync before keygen/keysign"
	TssBrokenMsg  = "tss share verification failed"
	InternalError = "fail to start the join party "
)

var (
	ErrHashFromOwner     = errors.New(" hash sent from data owner")
	ErrNotEnoughPeer     = errors.New("not enough nodes to evaluate hash")
	ErrNotMajority       = errors.New("message we received does not match the majority")
	ErrTssTimeOut        = errors.New("error Tss Timeout")
	ErrHashCheck         = errors.New("error in processing hash check")
	ErrHashInconsistency = errors.New("fail to agree on the hash value")
)

// PartyInfo the information used by tss key gen and key sign
type PartyInfo struct {
	PartyMap   *sync.Map
	PartyIDMap map[string]*btss.PartyID
}

type Node struct {
	Pubkey         string `json:"pubkey"`
	BlameData      []byte `json:"data"`
	BlameSignature []byte `json:"signature,omitempty"`
}

// Blame is used to store the blame nodes and the fail reason
// *** Blame struct had been referenced and registered in thornode , so please don't change this structure, otherwise it will have consensus failure when trying to update thornode  ***
type Blame struct {
	FailReason string `json:"fail_reason"`
	IsUnicast  bool   `json:"is_broadcast"`
	BlameNodes []Node `json:"blame_peers,omitempty"`
	blameLock  *sync.RWMutex
}
