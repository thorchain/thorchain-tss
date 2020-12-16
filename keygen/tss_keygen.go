package keygen

import (
	"errors"
	"fmt"
	"sync"
	"time"

	bcrypto "github.com/binance-chain/tss-lib/crypto"
	bkg "github.com/binance-chain/tss-lib/ecdsa/keygen"
	btss "github.com/binance-chain/tss-lib/tss"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	tcrypto "github.com/tendermint/tendermint/crypto"

	"gitlab.com/thorchain/tss/go-tss/blame"
	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/conversion"
	"gitlab.com/thorchain/tss/go-tss/messages"
	"gitlab.com/thorchain/tss/go-tss/p2p"
	"gitlab.com/thorchain/tss/go-tss/storage"
)

type TssKeyGen struct {
	logger          zerolog.Logger
	localNodePubKey string
	preParams       *bkg.LocalPreParams
	tssCommonStruct *common.TssCommon
	stopChan        chan struct{} // channel to indicate whether we should stop
	localParty      *btss.PartyID
	stateManager    storage.LocalStateManager
	commStopChan    chan struct{}
	p2pComm         *p2p.Communication
}

func NewTssKeyGen(localP2PID string,
	conf common.TssConfig,
	localNodePubKey string,
	broadcastChan chan *messages.BroadcastMsgChan,
	stopChan chan struct{},
	preParam *bkg.LocalPreParams,
	msgID string,
	stateManager storage.LocalStateManager,
	privateKey tcrypto.PrivKey,
	p2pComm *p2p.Communication) *TssKeyGen {
	return &TssKeyGen{
		logger: log.With().
			Str("module", "keygen").
			Str("msgID", msgID).Logger(),
		localNodePubKey: localNodePubKey,
		preParams:       preParam,
		tssCommonStruct: common.NewTssCommon(localP2PID, broadcastChan, conf, msgID, privateKey, 1),
		stopChan:        stopChan,
		localParty:      nil,
		stateManager:    stateManager,
		commStopChan:    make(chan struct{}),
		p2pComm:         p2pComm,
	}
}

func (tKeyGen *TssKeyGen) GetTssKeyGenChannels() chan *p2p.Message {
	return tKeyGen.tssCommonStruct.TssMsg
}

func (tKeyGen *TssKeyGen) GetTssCommonStruct() *common.TssCommon {
	return tKeyGen.tssCommonStruct
}

func (tKeyGen *TssKeyGen) GenerateNewKey(keygenReq Request) (*bcrypto.ECPoint, error) {
	partiesID, localPartyID, err := conversion.GetParties(keygenReq.Keys, tKeyGen.localNodePubKey)
	if err != nil {
		return nil, fmt.Errorf("fail to get keygen parties: %w", err)
	}

	keyGenLocalStateItem := storage.KeygenLocalState{
		ParticipantKeys: keygenReq.Keys,
		LocalPartyKey:   tKeyGen.localNodePubKey,
	}

	threshold, err := conversion.GetThreshold(len(partiesID))
	if err != nil {
		return nil, err
	}
	keyGenPartyMap := new(sync.Map)
	ctx := btss.NewPeerContext(partiesID)
	params := btss.NewParameters(ctx, localPartyID, len(partiesID), threshold)
	outCh := make(chan btss.Message, len(partiesID))
	endCh := make(chan bkg.LocalPartySaveData, len(partiesID))
	errChan := make(chan struct{})
	if tKeyGen.preParams == nil {
		tKeyGen.logger.Error().Err(err).Msg("error, empty pre-parameters")
		return nil, errors.New("error, empty pre-parameters")
	}
	blameMgr := tKeyGen.tssCommonStruct.GetBlameMgr()
	keyGenParty := bkg.NewLocalParty(params, outCh, endCh, *tKeyGen.preParams)
	partyIDMap := conversion.SetupPartyIDMap(partiesID)
	err1 := conversion.SetupIDMaps(partyIDMap, tKeyGen.tssCommonStruct.PartyIDtoP2PID)
	err2 := conversion.SetupIDMaps(partyIDMap, blameMgr.PartyIDtoP2PID)
	if err1 != nil || err2 != nil {
		tKeyGen.logger.Error().Msgf("error in creating mapping between partyID and P2P ID")
		return nil, err
	}
	// we never run multi keygen, so the moniker is set to default empty value
	keyGenPartyMap.Store("", keyGenParty)
	partyInfo := &common.PartyInfo{
		PartyMap:   keyGenPartyMap,
		PartyIDMap: partyIDMap,
	}

	tKeyGen.tssCommonStruct.SetPartyInfo(partyInfo)
	blameMgr.SetPartyInfo(keyGenPartyMap, partyIDMap)
	tKeyGen.tssCommonStruct.P2PPeersLock.Lock()
	tKeyGen.tssCommonStruct.P2PPeers = conversion.GetPeersID(tKeyGen.tssCommonStruct.PartyIDtoP2PID, tKeyGen.tssCommonStruct.GetLocalPeerID())
	tKeyGen.tssCommonStruct.P2PPeersLock.Unlock()
	var keyGenWg sync.WaitGroup
	keyGenWg.Add(2)
	// start keygen
	go func() {
		defer keyGenWg.Done()
		defer tKeyGen.logger.Debug().Msg(">>>>>>>>>>>>>.keyGenParty started")
		if err := keyGenParty.Start(); nil != err {
			tKeyGen.logger.Error().Err(err).Msg("fail to start keygen party")
			close(errChan)
		}
	}()
	go tKeyGen.tssCommonStruct.ProcessInboundMessages(tKeyGen.commStopChan, &keyGenWg)

	r, err := tKeyGen.processKeyGen(errChan, outCh, endCh, keyGenLocalStateItem)
	if err != nil {
		close(tKeyGen.commStopChan)
		return nil, fmt.Errorf("fail to process key sign: %w", err)
	}
	select {
	case <-time.After(time.Second * 5):
		close(tKeyGen.commStopChan)

	case <-tKeyGen.tssCommonStruct.GetTaskDone():
		close(tKeyGen.commStopChan)
	}

	keyGenWg.Wait()
	return r, err
}

func (tKeyGen *TssKeyGen) processKeyGen(errChan chan struct{},
	outCh <-chan btss.Message,
	endCh <-chan bkg.LocalPartySaveData,
	keyGenLocalStateItem storage.KeygenLocalState) (*bcrypto.ECPoint, error) {
	defer tKeyGen.logger.Debug().Msg("finished keygen process")
	tKeyGen.logger.Debug().Msg("start to read messages from local party")
	tssConf := tKeyGen.tssCommonStruct.GetConf()
	blameMgr := tKeyGen.tssCommonStruct.GetBlameMgr()
	for {
		select {
		case <-errChan: // when keyGenParty return
			tKeyGen.logger.Error().Msg("key gen failed")
			return nil, errors.New("error channel closed fail to start local party")

		case <-tKeyGen.stopChan: // when TSS processor receive signal to quit
			return nil, errors.New("received exit signal")

		case <-time.After(tssConf.KeyGenTimeout):
			// we bail out after KeyGenTimeoutSeconds
			tKeyGen.logger.Error().Msgf("fail to generate message with %s", tssConf.KeyGenTimeout.String())
			lastMsg := blameMgr.GetLastMsg()
			failReason := blameMgr.GetBlame().FailReason
			if failReason == "" {
				failReason = blame.TssTimeout
			}
			if lastMsg == nil {
				tKeyGen.logger.Error().Msg("fail to start the keygen, the last produced message of this node is none")
				return nil, errors.New("timeout before shared message is generated")
			}
			blameNodesUnicast, err := blameMgr.GetUnicastBlame(messages.KEYGEN2aUnicast)
			if err != nil {
				tKeyGen.logger.Error().Err(err).Msg("error in get unicast blame")
			}
			tKeyGen.tssCommonStruct.P2PPeersLock.RLock()
			threshold, err := conversion.GetThreshold(len(tKeyGen.tssCommonStruct.P2PPeers) + 1)
			tKeyGen.tssCommonStruct.P2PPeersLock.RUnlock()
			if err != nil {
				tKeyGen.logger.Error().Err(err).Msg("error in get the threshold to generate blame")
			}

			if len(blameNodesUnicast) > 0 && len(blameNodesUnicast) <= threshold {
				blameMgr.GetBlame().SetBlame(failReason, blameNodesUnicast, true)
			}
			blameNodesBroadcast, err := blameMgr.GetBroadcastBlame(lastMsg.Type())
			if err != nil {
				tKeyGen.logger.Error().Err(err).Msg("error in get broadcast blame")
			}
			blameMgr.GetBlame().AddBlameNodes(blameNodesBroadcast...)

			// if we cannot find the blame node, we check whether everyone send me the share
			if len(blameMgr.GetBlame().BlameNodes) == 0 {
				blameNodesMisingShare, isUnicast, err := blameMgr.TssMissingShareBlame(messages.TSSKEYGENROUNDS)
				if err != nil {
					tKeyGen.logger.Error().Err(err).Msg("fail to get the node of missing share ")
				}
				if len(blameNodesMisingShare) > 0 && len(blameNodesMisingShare) <= threshold {
					blameMgr.GetBlame().AddBlameNodes(blameNodesMisingShare...)
					blameMgr.GetBlame().IsUnicast = isUnicast
				}
			}
			return nil, blame.ErrTssTimeOut

		case msg := <-outCh:
			tKeyGen.logger.Debug().Msgf(">>>>>>>>>>msg: %s", msg.String())
			blameMgr.SetLastMsg(msg)
			err := tKeyGen.tssCommonStruct.ProcessOutCh(msg, messages.TSSKeyGenMsg)
			if err != nil {
				tKeyGen.logger.Error().Err(err).Msg("fail to process the message")
				return nil, err
			}

		case msg := <-endCh:
			tKeyGen.logger.Debug().Msgf("keygen finished successfully: %s", msg.ECDSAPub.Y().String())
			err := tKeyGen.tssCommonStruct.NotifyTaskDone()
			if err != nil {
				tKeyGen.logger.Error().Err(err).Msg("fail to broadcast the keysign done")
			}
			pubKey, _, err := conversion.GetTssPubKey(msg.ECDSAPub)
			if err != nil {
				return nil, fmt.Errorf("fail to get thorchain pubkey: %w", err)
			}
			keyGenLocalStateItem.LocalData = msg
			keyGenLocalStateItem.PubKey = pubKey
			if err := tKeyGen.stateManager.SaveLocalState(keyGenLocalStateItem); err != nil {
				return nil, fmt.Errorf("fail to save keygen result to storage: %w", err)
			}
			address := tKeyGen.p2pComm.ExportPeerAddress()
			if err := tKeyGen.stateManager.SaveAddressBook(address); err != nil {
				tKeyGen.logger.Error().Err(err).Msg("fail to save the peer addresses")
			}
			return msg.ECDSAPub, nil
		}
	}
}
