package keysign

import (
	bc "github.com/binance-chain/tss-lib/common"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/p2p"
	"gitlab.com/thorchain/tss/go-tss/storage"
)

type TssKeySign interface {
	GetTssKeySignChannels() chan *p2p.Message
	GetTssCommonStruct() *common.TssCommon
	SignMessage(msgToSign []byte, localStateItem storage.KeygenLocalState, parties []string) (*bc.SignatureData, error)
}
