package keysign

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"sync"
	"time"

	tsslibcommon "github.com/binance-chain/tss-lib/common"
	"github.com/binance-chain/tss-lib/ecdsa/signing"
	btss "github.com/binance-chain/tss-lib/tss"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	tcrypto "github.com/tendermint/tendermint/crypto"
	"go.uber.org/atomic"

	"gitlab.com/thorchain/tss/go-tss/blame"
	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/conversion"
	"gitlab.com/thorchain/tss/go-tss/messages"
	"gitlab.com/thorchain/tss/go-tss/p2p"
	"gitlab.com/thorchain/tss/go-tss/storage"
)

type TssKeySign struct {
	logger          zerolog.Logger
	tssCommonStruct *common.TssCommon
	stopChan        chan struct{} // channel to indicate whether we should stop
	localParties    []*btss.PartyID
	commStopChan    chan struct{}
	p2pComm         *p2p.Communication
	stateManager    storage.LocalStateManager
}

func NewTssKeySign(localP2PID string,
	conf common.TssConfig,
	broadcastChan chan *messages.BroadcastMsgChan,
	stopChan chan struct{}, msgID string, privKey tcrypto.PrivKey, p2pComm *p2p.Communication, stateManager storage.LocalStateManager, msgNum int) *TssKeySign {
	logItems := []string{"keySign", msgID}
	return &TssKeySign{
		logger:          log.With().Strs("module", logItems).Logger(),
		tssCommonStruct: common.NewTssCommon(localP2PID, broadcastChan, conf, msgID, privKey, msgNum),
		stopChan:        stopChan,
		localParties:    make([]*btss.PartyID, 0),
		commStopChan:    make(chan struct{}),
		p2pComm:         p2pComm,
		stateManager:    stateManager,
	}
}

func (tKeySign *TssKeySign) GetTssKeySignChannels() chan *p2p.Message {
	return tKeySign.tssCommonStruct.TssMsg
}

func (tKeySign *TssKeySign) GetTssCommonStruct() *common.TssCommon {
	return tKeySign.tssCommonStruct
}

func (tKeySign *TssKeySign) startBatchSigning(keySignPartyMap *sync.Map, msgNum int) bool {
	// start the batch sign
	var keySignWg sync.WaitGroup
	ret := atomic.NewBool(true)
	keySignWg.Add(msgNum)
	keySignPartyMap.Range(func(key, value interface{}) bool {
		eachParty := value.(btss.Party)
		go func(eachParty btss.Party) {
			defer keySignWg.Done()
			if err := eachParty.Start(); err != nil {
				tKeySign.logger.Error().Err(err).Msg("fail to start key sign party")
				ret.Store(false)
			}
			tKeySign.logger.Info().Msgf("local party(%s) %s is ready", eachParty.PartyID().Id, eachParty.PartyID().Moniker)
		}(eachParty)
		return true
	})
	keySignWg.Wait()
	return ret.Load()
}

// signMessage
func (tKeySign *TssKeySign) SignMessage(msgsToSign [][]byte, localStateItem storage.KeygenLocalState, parties []string) ([]*tsslibcommon.ECSignature, error) {
	partiesID, localPartyID, err := conversion.GetParties(parties, localStateItem.LocalPartyKey)
	if err != nil {
		return nil, fmt.Errorf("fail to form key sign party: %w", err)
	}
	if !common.Contains(partiesID, localPartyID) {
		tKeySign.logger.Info().Msgf("we are not in this rounds key sign")
		return nil, nil
	}
	threshold, err := conversion.GetThreshold(len(localStateItem.ParticipantKeys))
	if err != nil {
		return nil, errors.New("fail to get threshold")
	}

	// tKeySign.logger.Debug().Msgf("local party: %+v", localPartyID)
	outCh := make(chan btss.Message, 2*len(partiesID)*len(msgsToSign))
	endCh := make(chan *signing.SignatureData, len(partiesID)*len(msgsToSign))
	errCh := make(chan struct{})

	keySignPartyMap := new(sync.Map)
	for i, val := range msgsToSign {
		m, err := common.MsgToHashInt(val)
		if err != nil {
			return nil, fmt.Errorf("fail to convert msg to hash int: %w", err)
		}
		moniker := m.String() + ":" + strconv.Itoa(i)
		partiesID, eachLocalPartyID, err := conversion.GetParties(parties, localStateItem.LocalPartyKey)
		ctx := btss.NewPeerContext(partiesID)
		if err != nil {
			return nil, fmt.Errorf("error to create parties in batch signging %w\n", err)
		}
		eachLocalPartyID.Moniker = moniker
		tKeySign.localParties = nil
		params := btss.NewParameters(ctx, eachLocalPartyID, len(partiesID), threshold)
		keySignParty := signing.NewLocalParty(m, params, localStateItem.LocalData, outCh, endCh)
		keySignPartyMap.Store(moniker, keySignParty)
	}

	blameMgr := tKeySign.tssCommonStruct.GetBlameMgr()
	partyIDMap := conversion.SetupPartyIDMap(partiesID)
	err1 := conversion.SetupIDMaps(partyIDMap, tKeySign.tssCommonStruct.PartyIDtoP2PID)
	err2 := conversion.SetupIDMaps(partyIDMap, blameMgr.PartyIDtoP2PID)
	if err1 != nil || err2 != nil {
		tKeySign.logger.Error().Err(err).Msgf("error in creating mapping between partyID and P2P ID")
		return nil, err
	}

	tKeySign.tssCommonStruct.SetPartyInfo(&common.PartyInfo{
		PartyMap:   keySignPartyMap,
		PartyIDMap: partyIDMap,
	})

	blameMgr.SetPartyInfo(keySignPartyMap, partyIDMap)

	tKeySign.tssCommonStruct.P2PPeersLock.Lock()
	tKeySign.tssCommonStruct.P2PPeers = conversion.GetPeersID(tKeySign.tssCommonStruct.PartyIDtoP2PID, tKeySign.tssCommonStruct.GetLocalPeerID())
	tKeySign.tssCommonStruct.P2PPeersLock.Unlock()
	var keySignWg sync.WaitGroup
	keySignWg.Add(2)
	// start the key sign
	go func() {
		defer keySignWg.Done()
		ret := tKeySign.startBatchSigning(keySignPartyMap, len(msgsToSign))
		if !ret {
			close(errCh)
		}
	}()
	go tKeySign.tssCommonStruct.ProcessInboundMessages(tKeySign.commStopChan, &keySignWg)
	results, err := tKeySign.processKeySign(len(msgsToSign), errCh, outCh, endCh)
	if err != nil {
		close(tKeySign.commStopChan)
		return nil, fmt.Errorf("fail to process key sign: %w", err)
	}

	select {
	case <-time.After(time.Second * 5):
		close(tKeySign.commStopChan)
	case <-tKeySign.tssCommonStruct.GetTaskDone():
		close(tKeySign.commStopChan)
	}
	keySignWg.Wait()

	tKeySign.logger.Info().Msgf("%s successfully sign the message", tKeySign.p2pComm.GetHost().ID().String())
	sort.SliceStable(results, func(i, j int) bool {
		a := new(big.Int).SetBytes(results[i].M)
		b := new(big.Int).SetBytes(results[j].M)

		if a.Cmp(b) == -1 {
			return false
		}
		return true
	})

	return results, nil
}

func (tKeySign *TssKeySign) processKeySign(reqNum int, errChan chan struct{}, outCh <-chan btss.Message, endCh <-chan *signing.SignatureData) ([]*tsslibcommon.ECSignature, error) {
	defer tKeySign.logger.Debug().Msg("key sign finished")
	tKeySign.logger.Debug().Msg("start to read messages from local party")
	var signatures []*tsslibcommon.ECSignature

	tssConf := tKeySign.tssCommonStruct.GetConf()
	blameMgr := tKeySign.tssCommonStruct.GetBlameMgr()

	for {
		select {
		case <-errChan: // when key sign return
			tKeySign.logger.Error().Msg("key sign failed")
			return nil, errors.New("error channel closed fail to start local party")
		case <-tKeySign.stopChan: // when TSS processor receive signal to quit
			return nil, errors.New("received exit signal")
		case <-time.After(tssConf.KeySignTimeout):
			// we bail out after KeySignTimeoutSeconds
			tKeySign.logger.Error().Msgf("fail to sign message with %s", tssConf.KeySignTimeout.String())
			lastMsg := blameMgr.GetLastMsg()
			failReason := blameMgr.GetBlame().FailReason
			if failReason == "" {
				failReason = blame.TssTimeout
			}

			tKeySign.tssCommonStruct.P2PPeersLock.RLock()
			threshold, err := conversion.GetThreshold(len(tKeySign.tssCommonStruct.P2PPeers) + 1)
			tKeySign.tssCommonStruct.P2PPeersLock.RUnlock()
			if err != nil {
				tKeySign.logger.Error().Err(err).Msg("error in get the threshold for generate blame")
			}
			if !lastMsg.IsBroadcast() {
				blameNodesUnicast, err := blameMgr.GetUnicastBlame(lastMsg.Type())
				if err != nil {
					tKeySign.logger.Error().Err(err).Msg("error in get unicast blame")
				}
				if len(blameNodesUnicast) > 0 && len(blameNodesUnicast) <= threshold {
					blameMgr.GetBlame().SetBlame(failReason, blameNodesUnicast, true)
				}
			} else {
				blameNodesUnicast, err := blameMgr.GetUnicastBlame(conversion.GetPreviousKeySignUicast(lastMsg.Type()))
				if err != nil {
					tKeySign.logger.Error().Err(err).Msg("error in get unicast blame")
				}
				if len(blameNodesUnicast) > 0 && len(blameNodesUnicast) <= threshold {
					blameMgr.GetBlame().SetBlame(failReason, blameNodesUnicast, true)
				}
			}

			blameNodesBroadcast, err := blameMgr.GetBroadcastBlame(lastMsg.Type())
			if err != nil {
				tKeySign.logger.Error().Err(err).Msg("error in get broadcast blame")
			}
			blameMgr.GetBlame().AddBlameNodes(blameNodesBroadcast...)

			// if we cannot find the blame node, we check whether everyone send me the share
			if len(blameMgr.GetBlame().BlameNodes) == 0 {
				blameNodesMisingShare, isUnicast, err := blameMgr.TssMissingShareBlame(messages.TSSKEYSIGNROUNDS)
				if err != nil {
					tKeySign.logger.Error().Err(err).Msg("fail to get the node of missing share ")
				}

				if len(blameNodesMisingShare) > 0 && len(blameNodesMisingShare) <= threshold {
					blameMgr.GetBlame().AddBlameNodes(blameNodesMisingShare...)
					blameMgr.GetBlame().IsUnicast = isUnicast
				}
			}

			return nil, blame.ErrTssTimeOut
		case msg := <-outCh:
			tKeySign.logger.Debug().Msgf(">>>>>>>>>>key sign msg: %s", msg.String())
			tKeySign.tssCommonStruct.GetBlameMgr().SetLastMsg(msg)
			err := tKeySign.tssCommonStruct.ProcessOutCh(msg, messages.TSSKeySignMsg)
			if err != nil {
				return nil, err
			}

		case msg := <-endCh:
			signatures = append(signatures, msg.GetSignature())
			if len(signatures) == reqNum {
				tKeySign.logger.Debug().Msg("we have done the key sign")
				err := tKeySign.tssCommonStruct.NotifyTaskDone()
				if err != nil {
					tKeySign.logger.Error().Err(err).Msg("fail to broadcast the keysign done")
				}
				//export the address book
				address := tKeySign.p2pComm.ExportPeerAddress()
				if err := tKeySign.stateManager.SaveAddressBook(address); err != nil {
					tKeySign.logger.Error().Err(err).Msg("fail to save the peer addresses")
				}
				return signatures, nil
			}
		}
	}
}
