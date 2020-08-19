package keygen

import (
	bcrypto "github.com/binance-chain/tss-lib/crypto"
)

type TssKeyGen interface {
	// NewTssKeyGen(localP2PID string, conf common.TssConfig, localNodePubKey string, broadcastChan chan *messages.BroadcastMsgChan, stopChan chan struct{}, preParam *bkg.LocalPreParams, msgID string, stateManager storage.LocalStateManager, privateKey tcrypto.PrivKey, p2pComm *p2p.Communication) *TssKeyGen
	GenerateNewKey(keygenReq Request) (*bcrypto.ECPoint, error)
}
