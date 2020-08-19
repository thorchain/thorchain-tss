package keygen

import (
	bcrypto "github.com/binance-chain/tss-lib/crypto"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

//type KeyGen struct {
//	logger          zerolog.Logger
//	localNodePubKey string
//	preParams       *bkg.LocalPreParams
//	tssCommonStruct *common.TssCommon
//	stopChan        chan struct{} // channel to indicate whether we should stop
//	localParty      *btss.PartyID
//	stateManager    storage.LocalStateManager
//	commStopChan    chan struct{}
//	p2pComm         *p2p.Communication
//}
//
//func NewTssKeyGen(localP2PID string,
//	conf common.TssConfig,
//	localNodePubKey string,
//	broadcastChan chan *messages.BroadcastMsgChan,
//	stopChan chan struct{},
//	preParam *bkg.LocalPreParams,
//	msgID string,
//	stateManager storage.LocalStateManager,
//	privateKey tcrypto.PrivKey,
//	p2pComm *p2p.Communication) *KeyGen {
//	return &KeyGen{
//		logger: log.With().
//			Str("module", "keygen").
//			Str("msgID", msgID).Logger(),
//		localNodePubKey: localNodePubKey,
//		preParams:       preParam,
//		tssCommonStruct: common.NewTssCommon(localP2PID, broadcastChan, conf, msgID, privateKey),
//		stopChan:        stopChan,
//		localParty:      nil,
//		stateManager:    stateManager,
//		commStopChan:    make(chan struct{}),
//		p2pComm:         p2pComm,
//	}
//}

type TssKeyGen interface {
	// NewTssKeyGen(localP2PID string, conf common.TssConfig, localNodePubKey string, broadcastChan chan *messages.BroadcastMsgChan, stopChan chan struct{}, preParam *bkg.LocalPreParams, msgID string, stateManager storage.LocalStateManager, privateKey tcrypto.PrivKey, p2pComm *p2p.Communication) *TssKeyGen
	GenerateNewKey(keygenReq Request) (*bcrypto.ECPoint, error)
	GetTssKeyGenChannels() chan *p2p.Message
	GetTssCommonStruct() *common.TssCommon
}
