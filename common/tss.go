package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"

	btss "github.com/binance-chain/tss-lib/tss"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	tcrypto "github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/secp256k1"

	"gitlab.com/thorchain/tss/go-tss/blame"
	"gitlab.com/thorchain/tss/go-tss/conversion"
	"gitlab.com/thorchain/tss/go-tss/messages"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

// PartyInfo the information used by tss key gen and key sign
type PartyInfo struct {
	PartyMap   *sync.Map
	PartyIDMap map[string]*btss.PartyID
}

type TssCommon struct {
	conf                        TssConfig
	logger                      zerolog.Logger
	partyLock                   *sync.Mutex
	partyInfo                   *PartyInfo
	PartyIDtoP2PID              map[string]peer.ID
	unConfirmedMsgLock          *sync.Mutex
	unConfirmedMessages         map[string]*LocalCacheItem
	localPeerID                 string
	broadcastChannel            chan *messages.BroadcastMsgChan
	TssMsg                      chan *p2p.Message
	P2PPeersLock                *sync.RWMutex
	P2PPeers                    []peer.ID // most of tss message are broadcast, we store the peers ID to avoid iterating
	msgID                       string
	privateKey                  tcrypto.PrivKey
	taskDone                    chan struct{}
	blameMgr                    *blame.Manager
	finishedPeers               map[string]bool
	culprits                    []*btss.PartyID
	culpritsLock                *sync.RWMutex
	cachedWireBroadcastMsgLists *sync.Map
	cachedWireUnicastMsgLists   *sync.Map
	msgNum                      int
}

func NewTssCommon(peerID string, broadcastChannel chan *messages.BroadcastMsgChan, conf TssConfig, msgID string, privKey tcrypto.PrivKey, msgNum int) *TssCommon {
	return &TssCommon{
		conf:                        conf,
		logger:                      log.With().Str("module", "tsscommon").Logger(),
		partyLock:                   &sync.Mutex{},
		partyInfo:                   nil,
		PartyIDtoP2PID:              make(map[string]peer.ID),
		unConfirmedMsgLock:          &sync.Mutex{},
		unConfirmedMessages:         make(map[string]*LocalCacheItem),
		broadcastChannel:            broadcastChannel,
		TssMsg:                      make(chan *p2p.Message, msgNum),
		P2PPeersLock:                &sync.RWMutex{},
		P2PPeers:                    nil,
		msgID:                       msgID,
		localPeerID:                 peerID,
		privateKey:                  privKey,
		taskDone:                    make(chan struct{}),
		blameMgr:                    blame.NewBlameManager(),
		finishedPeers:               make(map[string]bool),
		culpritsLock:                &sync.RWMutex{},
		cachedWireBroadcastMsgLists: &sync.Map{},
		cachedWireUnicastMsgLists:   &sync.Map{},
		msgNum:                      msgNum,
	}
}

type BulkWireMsg struct {
	WiredBulkMsgs []byte
	MsgIdentifier string
	Routing       *btss.MessageRouting
}

func NewBulkWireMsg(msg []byte, id string, r *btss.MessageRouting) BulkWireMsg {
	return BulkWireMsg{
		WiredBulkMsgs: msg,
		MsgIdentifier: id,
		Routing:       r,
	}
}

type tssJob struct {
	wireBytes      []byte
	msgIdentifier  string
	partyID        *btss.PartyID
	isBroadcast    bool
	localParty     btss.Party
	acceptedShares map[blame.RoundInfo][]string
}

func newJob(party btss.Party, wireBytes []byte, msgIdentifier string, from *btss.PartyID, isBroadcast bool) *tssJob {
	return &tssJob{
		wireBytes:     wireBytes,
		msgIdentifier: msgIdentifier,
		partyID:       from,
		isBroadcast:   isBroadcast,
		localParty:    party,
	}
}

func (t *TssCommon) doTssJob(tssJobChan chan *tssJob, jobWg *sync.WaitGroup) {
	defer func() {
		jobWg.Done()
	}()

	for tssjob := range tssJobChan {
		party := tssjob.localParty
		wireBytes := tssjob.wireBytes
		partyID := tssjob.partyID
		isBroadcast := tssjob.isBroadcast

		round, err := GetMsgRound(wireBytes, partyID, isBroadcast)
		if err != nil {
			t.logger.Error().Err(err).Msg("broken tss share")
			continue
		}
		round.MsgIdentifier = tssjob.msgIdentifier

		_, errUp := party.UpdateFromBytes(wireBytes, partyID, isBroadcast)
		if errUp != nil {
			err := t.processInvalidMsgBlame(round.RoundMsg, round, errUp)
			t.logger.Error().Err(err).Msgf("fail to apply the share to tss")
			continue
		}
		// we need to retrieve the partylist again as others may update it once we process apply tss share
		t.blameMgr.UpdateAcceptShare(round, partyID.Id)
	}
}

func (t *TssCommon) renderToP2P(broadcastMsg *messages.BroadcastMsgChan) {
	if t.broadcastChannel == nil {
		t.logger.Warn().Msg("broadcast channel is not set")
		return
	}
	t.broadcastChannel <- broadcastMsg
}

// GetConf get current configuration for Tss
func (t *TssCommon) GetConf() TssConfig {
	return t.conf
}

func (t *TssCommon) GetTaskDone() chan struct{} {
	return t.taskDone
}

func (t *TssCommon) GetBlameMgr() *blame.Manager {
	return t.blameMgr
}

func (t *TssCommon) SetPartyInfo(partyInfo *PartyInfo) {
	t.partyLock.Lock()
	defer t.partyLock.Unlock()
	t.partyInfo = partyInfo
}

func (t *TssCommon) getPartyInfo() *PartyInfo {
	t.partyLock.Lock()
	defer t.partyLock.Unlock()
	return t.partyInfo
}

func (t *TssCommon) GetLocalPeerID() string {
	return t.localPeerID
}

func (t *TssCommon) SetLocalPeerID(peerID string) {
	t.localPeerID = peerID
}

func (t *TssCommon) processInvalidMsgBlame(roundInfo string, round blame.RoundInfo, err *btss.Error) error {
	// now we get the culprits ID, invalid message and signature the culprits sent
	var culpritsID []string
	var invalidMsgs []*messages.WireMessage
	unicast := checkUnicast(round)
	t.culpritsLock.Lock()
	t.culprits = append(t.culprits, err.Culprits()...)
	t.culpritsLock.Unlock()
	for _, el := range err.Culprits() {
		culpritsID = append(culpritsID, el.Id)
		key := fmt.Sprintf("%s-%s", el.Id, roundInfo)
		storedMsg := t.blameMgr.GetRoundMgr().Get(key)
		invalidMsgs = append(invalidMsgs, storedMsg)
	}
	pubkeys, errBlame := conversion.AccPubKeysFromPartyIDs(culpritsID, t.partyInfo.PartyIDMap)
	if errBlame != nil {
		t.logger.Error().Err(err.Cause()).Msgf("error in get the blame nodes")
		t.blameMgr.GetBlame().SetBlame(blame.TssBrokenMsg, nil, unicast)
		return fmt.Errorf("error in getting the blame nodes")
	}
	// This error indicates the share is wrong, we include this signature to prove that
	// this incorrect share is from the share owner.
	var blameNodes []blame.Node
	var msgBody, sig []byte
	for i, pk := range pubkeys {
		invalidMsg := invalidMsgs[i]
		if invalidMsg == nil {
			t.logger.Error().Msg("we cannot find the record of this curlprit, set it as blank")
			msgBody = []byte{}
			sig = []byte{}
		} else {
			msgBody = invalidMsg.Message
			sig = invalidMsg.Sig
		}
		blameNodes = append(blameNodes, blame.NewNode(pk, msgBody, sig))
	}
	t.blameMgr.GetBlame().SetBlame(blame.TssBrokenMsg, blameNodes, unicast)
	return fmt.Errorf("fail to set bytes to local party: %w", err)
}

// updateLocal will apply the wireMsg to local keygen/keysign party
func (t *TssCommon) updateLocal(wireMsg *messages.WireMessage) error {
	if wireMsg == nil || wireMsg.Routing == nil || wireMsg.Routing.From == nil {
		t.logger.Warn().Msg("wire msg is nil")
		return errors.New("invalid wireMsg")
	}
	partyInfo := t.getPartyInfo()
	if partyInfo == nil {
		return nil
	}
	partyID, ok := partyInfo.PartyIDMap[wireMsg.Routing.From.Id]
	if !ok {
		return fmt.Errorf("get message from unknown party %s", partyID.Id)
	}

	dataOwnerPeerID, ok := t.PartyIDtoP2PID[wireMsg.Routing.From.Id]
	if !ok {
		t.logger.Error().Msg("fail to find the peer ID of this party")
		return errors.New("fail to find the peer")
	}
	// here we log down this peer as the latest unicast peer
	if !wireMsg.Routing.IsBroadcast {
		t.blameMgr.SetLastUnicastPeer(dataOwnerPeerID, wireMsg.RoundInfo)
	}

	var bulkMsg []BulkWireMsg
	err := json.Unmarshal(wireMsg.Message, &bulkMsg)
	if err != nil {
		t.logger.Error().Err(err).Msg("error to unmarshal the BulkMsg")
		return err
	}

	worker := runtime.NumCPU()
	tssJobChan := make(chan *tssJob, len(bulkMsg))
	jobWg := sync.WaitGroup{}
	for i := 0; i < worker; i++ {
		jobWg.Add(1)
		go t.doTssJob(tssJobChan, &jobWg)
	}
	for _, msg := range bulkMsg {
		data, ok := partyInfo.PartyMap.Load(msg.MsgIdentifier)
		if !ok {
			t.logger.Error().Msg("cannot find the party to this wired msg")
			return errors.New("cannot find the party")
		}
		localMsgParty := data.(btss.Party)
		partyID, ok := partyInfo.PartyIDMap[msg.Routing.From.Id]
		if !ok {
			t.logger.Error().Msg("error in find the partyID")
			return errors.New("cannot find the party to handle the message")
		}

		round, err := GetMsgRound(msg.WiredBulkMsgs, partyID, msg.Routing.IsBroadcast)
		if err != nil {
			t.logger.Error().Err(err).Msg("broken tss share")
			return err
		}

		// we only allow a message be updated only once.
		// here we use round + msgIdentifier as the key for the acceptedShares
		round.MsgIdentifier = msg.MsgIdentifier
		// if this share is duplicated, we skip this share
		if t.blameMgr.CheckMsgDuplication(round, partyID.Id) {
			t.logger.Debug().Msgf("we received the duplicated message from party %s", partyID.Id)
			continue
		}

		partyInlist := func(el *btss.PartyID, l []*btss.PartyID) bool {
			for _, each := range l {
				if el == each {
					return true
				}
			}
			return false
		}
		t.culpritsLock.RLock()
		if len(t.culprits) != 0 && partyInlist(partyID, t.culprits) {
			t.logger.Error().Msgf("the malicious party (party ID:%s) try to send incorrect message to me (party ID:%s)", partyID.Id, localMsgParty.PartyID().Id)
			t.culpritsLock.RUnlock()
			return errors.New(blame.TssBrokenMsg)
		}
		t.culpritsLock.RUnlock()
		job := newJob(localMsgParty, msg.WiredBulkMsgs, round.MsgIdentifier, partyID, msg.Routing.IsBroadcast)
		tssJobChan <- job
	}
	close(tssJobChan)
	jobWg.Wait()
	return nil
}

func (t *TssCommon) checkDupAndUpdateVerMsg(bMsg *messages.BroadcastConfirmMessage, peerID string) bool {
	localCacheItem := t.TryGetLocalCacheItem(bMsg.Key)
	// we check whether this node has already sent the VerMsg message to avoid eclipse of others VerMsg
	if localCacheItem == nil {
		bMsg.P2PID = peerID
		return true
	}

	localCacheItem.lock.Lock()
	defer localCacheItem.lock.Unlock()
	if _, ok := localCacheItem.ConfirmedList[peerID]; ok {
		return false
	}
	bMsg.P2PID = peerID
	return true
}

func (t *TssCommon) ProcessOneMessage(wrappedMsg *messages.WrappedMessage, peerID string) error {
	t.logger.Debug().Msg("start process one message")
	defer t.logger.Debug().Msg("finish processing one message")
	if nil == wrappedMsg {
		return errors.New("invalid wireMessage")
	}

	switch wrappedMsg.MessageType {
	case messages.TSSKeyGenMsg, messages.TSSKeySignMsg:
		var wireMsg messages.WireMessage
		if err := json.Unmarshal(wrappedMsg.Payload, &wireMsg); nil != err {
			return fmt.Errorf("fail to unmarshal wire message: %w", err)
		}
		return t.processTSSMsg(&wireMsg, wrappedMsg.MessageType, false)
	case messages.TSSKeyGenVerMsg, messages.TSSKeySignVerMsg:
		var bMsg messages.BroadcastConfirmMessage
		if err := json.Unmarshal(wrappedMsg.Payload, &bMsg); nil != err {
			return errors.New("fail to unmarshal broadcast confirm message")
		}
		// we check whether this peer has already send us the VerMsg before update
		ret := t.checkDupAndUpdateVerMsg(&bMsg, peerID)
		if ret {
			return t.processVerMsg(&bMsg, wrappedMsg.MessageType)
		}
	case messages.TSSTaskDone:
		var wireMsg messages.TssTaskNotifier
		err := json.Unmarshal(wrappedMsg.Payload, &wireMsg)
		if err != nil {
			t.logger.Error().Err(err).Msg("fail to unmarshal the notify message")
			return nil
		}
		if wireMsg.TaskDone {
			// if we have already logged this node, we return to avoid close of a close channel
			if t.finishedPeers[peerID] {
				return fmt.Errorf("duplicated notification from peer %s ignored", peerID)
			}
			t.finishedPeers[peerID] = true
			if len(t.finishedPeers) == len(t.partyInfo.PartyIDMap)-1 {
				t.logger.Debug().Msg("we get the confirm of the nodes that generate the signature")
				close(t.taskDone)
			}
			return nil
		}
	case messages.TSSControlMsg:
		var wireMsg messages.TssControl
		if err := json.Unmarshal(wrappedMsg.Payload, &wireMsg); nil != err {
			return fmt.Errorf("fail to unmarshal wire message: %w", err)
		}
		if wireMsg.Msg == nil {
			decodedPeerID, err := peer.Decode(peerID)
			if err != nil {
				t.logger.Error().Err(err).Msg("error in decode the peer")
				return err
			}
			return t.processRequestMsgFromPeer([]peer.ID{decodedPeerID}, &wireMsg, false)
		}
		exist := t.blameMgr.GetShareMgr().QueryAndDelete(wireMsg.ReqHash)
		if !exist {
			t.logger.Debug().Msg("this request does not exit, maybe already processed")
			return nil
		}
		t.logger.Debug().Msg("we got the missing share from the peer")
		return t.processTSSMsg(wireMsg.Msg, wireMsg.RequestType, true)
	}

	return nil
}

func (t *TssCommon) getMsgHash(localCacheItem *LocalCacheItem, threshold int) (string, error) {
	hash, freq, err := getHighestFreq(localCacheItem.ConfirmedList)
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to get the hash freq")
		return "", blame.ErrHashCheck
	}
	if freq < threshold-1 {
		t.logger.Debug().Msgf("fail to have more than 2/3 peers agree on the received message threshold(%d)--total confirmed(%d)\n", threshold, freq)
		return "", blame.ErrHashInconsistency
	}
	return hash, nil
}

func (t *TssCommon) hashCheck(localCacheItem *LocalCacheItem, threshold int) error {
	dataOwner := localCacheItem.Msg.Routing.From
	dataOwnerP2PID, ok := t.PartyIDtoP2PID[dataOwner.Id]
	if !ok {
		t.logger.Warn().Msgf("error in find the data Owner P2PID\n")
		return errors.New("error in find the data Owner P2PID")
	}

	if localCacheItem.TotalConfirmParty() < threshold {
		t.logger.Debug().Msg("not enough nodes to evaluate the hash")
		return blame.ErrNotEnoughPeer
	}
	localCacheItem.lock.Lock()
	defer localCacheItem.lock.Unlock()

	targetHashValue := localCacheItem.Hash
	for P2PID := range localCacheItem.ConfirmedList {
		if P2PID == dataOwnerP2PID.String() {
			t.logger.Warn().Msgf("we detect that the data owner try to send the hash for his own message\n")
			delete(localCacheItem.ConfirmedList, P2PID)
			return blame.ErrHashFromOwner
		}
	}
	hash, err := t.getMsgHash(localCacheItem, threshold)
	if err != nil {
		return err
	}
	if targetHashValue == hash {
		t.logger.Debug().Msgf("hash check complete for messageID: %v", t.msgID)
		return nil
	}
	return blame.ErrNotMajority
}

func (t *TssCommon) sendBulkMsg(wiredMsgType string, tssMsgType messages.THORChainTSSMessageType, wiredMsgList []BulkWireMsg) error {
	// since all the messages in the list is the same round, so it must have the same dest
	// we just need to get the routing info of the first message
	r := wiredMsgList[0].Routing

	buf, err := json.Marshal(wiredMsgList)
	if err != nil {
		return fmt.Errorf("error in marshal the cachedWireMsg: %w", err)
	}

	sig, err := generateSignature(buf, t.msgID, t.privateKey)
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to generate the share's signature")
		return err
	}

	wireMsg := messages.WireMessage{
		Routing:   r,
		RoundInfo: wiredMsgType,
		Message:   buf,
		Sig:       sig,
	}
	wireMsgBytes, err := json.Marshal(wireMsg)
	if err != nil {
		return fmt.Errorf("fail to convert tss msg to wire bytes: %w", err)
	}
	wrappedMsg := messages.WrappedMessage{
		MsgID:       t.msgID,
		MessageType: tssMsgType,
		Payload:     wireMsgBytes,
	}

	peerIDs := make([]peer.ID, 0)
	if len(r.To) == 0 {
		t.P2PPeersLock.RLock()
		peerIDs = t.P2PPeers
		t.P2PPeersLock.RUnlock()
	} else {
		for _, each := range r.To {
			peerID, ok := t.PartyIDtoP2PID[each.Id]
			if !ok {
				t.logger.Error().Msg("error in find the P2P ID")
				continue
			}
			peerIDs = append(peerIDs, peerID)
		}
	}
	t.renderToP2P(&messages.BroadcastMsgChan{
		WrappedMessage: wrappedMsg,
		PeersID:        peerIDs,
	})

	return nil
}

func (t *TssCommon) ProcessOutCh(msg btss.Message, msgType messages.THORChainTSSMessageType) error {
	msgData, r, err := msg.WireBytes()
	// if we cannot get the wire share, the tss will fail, we just quit.
	if err != nil {
		return fmt.Errorf("fail to get wire bytes: %w", err)
	}

	if r.IsBroadcast {
		cachedWiredMsg := NewBulkWireMsg(msgData, msg.GetFrom().Moniker, r)
		// now we store this message in cache
		dat, ok := t.cachedWireBroadcastMsgLists.Load(msg.Type())
		if !ok {
			l := []BulkWireMsg{cachedWiredMsg}
			t.cachedWireBroadcastMsgLists.Store(msg.Type(), l)
		} else {
			cachedList := dat.([]BulkWireMsg)
			cachedList = append(cachedList, cachedWiredMsg)
			t.cachedWireBroadcastMsgLists.Store(msg.Type(), cachedList)
		}
	} else {
		cachedWiredMsg := NewBulkWireMsg(msgData, msg.GetFrom().Moniker, r)
		dat, ok := t.cachedWireUnicastMsgLists.Load(msg.Type() + ":" + r.To[0].String())
		if !ok {
			l := []BulkWireMsg{cachedWiredMsg}
			t.cachedWireUnicastMsgLists.Store(msg.Type()+":"+r.To[0].String(), l)
		} else {
			cachedList := dat.([]BulkWireMsg)
			cachedList = append(cachedList, cachedWiredMsg)
			t.cachedWireUnicastMsgLists.Store(msg.Type()+":"+r.To[0].String(), cachedList)
		}
	}
	t.cachedWireUnicastMsgLists.Range(func(key, value interface{}) bool {
		wiredMsgList := value.([]BulkWireMsg)
		ret := strings.Split(key.(string), ":")
		wiredMsgType := ret[0]
		if len(wiredMsgList) == t.msgNum {
			err := t.sendBulkMsg(wiredMsgType, msgType, wiredMsgList)
			if err != nil {
				t.logger.Error().Err(err).Msg("error in send bulk message")
				return true
			}
			t.cachedWireUnicastMsgLists.Delete(key)
		}
		return true
	})

	t.cachedWireBroadcastMsgLists.Range(func(key, value interface{}) bool {
		wiredMsgList := value.([]BulkWireMsg)
		wiredMsgType := key.(string)
		if len(wiredMsgList) == t.msgNum {
			err := t.sendBulkMsg(wiredMsgType, msgType, wiredMsgList)
			if err != nil {
				t.logger.Error().Err(err).Msg("error in send bulk message")
				return true
			}
			t.cachedWireBroadcastMsgLists.Delete(key)
		}
		return true
	})

	return nil
}

func (t *TssCommon) applyShare(localCacheItem *LocalCacheItem, threshold int, key string, msgType messages.THORChainTSSMessageType) error {
	unicast := true
	if localCacheItem.Msg.Routing.IsBroadcast {
		unicast = false
	}
	err := t.hashCheck(localCacheItem, threshold)
	if err != nil {
		if errors.Is(err, blame.ErrNotEnoughPeer) {
			return nil
		}
		if errors.Is(err, blame.ErrNotMajority) {
			t.logger.Error().Err(err).Msg("we send request to get the message match with majority")
			localCacheItem.Msg = nil
			return t.requestShareFromPeer(localCacheItem, threshold, key, msgType)
		}
		blamePk, err := t.blameMgr.TssWrongShareBlame(localCacheItem.Msg)
		if err != nil {
			t.logger.Error().Err(err).Msgf("error in get the blame nodes")
			t.blameMgr.GetBlame().SetBlame(blame.HashCheckFail, nil, unicast)
			return fmt.Errorf("error in getting the blame nodes %w", blame.ErrHashCheck)
		}
		blameNode := blame.NewNode(blamePk, localCacheItem.Msg.Message, localCacheItem.Msg.Sig)
		t.blameMgr.GetBlame().SetBlame(blame.HashCheckFail, []blame.Node{blameNode}, unicast)
		return blame.ErrHashCheck
	}

	t.blameMgr.GetRoundMgr().Set(key, localCacheItem.Msg)
	if err := t.updateLocal(localCacheItem.Msg); nil != err {
		return fmt.Errorf("fail to update the message to local party: %w", err)
	}
	t.logger.Debug().Msgf("remove key: %s", key)
	// the information had been confirmed by all party , we don't need it anymore
	t.removeKey(key)
	return nil
}

func (t *TssCommon) requestShareFromPeer(localCacheItem *LocalCacheItem, threshold int, key string, msgType messages.THORChainTSSMessageType) error {
	targetHash, err := t.getMsgHash(localCacheItem, threshold)
	if err != nil {
		t.logger.Debug().Msg("we do not know which message to request, so we quit")
		return nil
	}
	var peersIDs []peer.ID
	for thisPeerStr, hash := range localCacheItem.ConfirmedList {
		if hash == targetHash {
			thisPeer, err := peer.Decode(thisPeerStr)
			if err != nil {
				t.logger.Error().Err(err).Msg("fail to convert the p2p id")
				return err
			}
			peersIDs = append(peersIDs, thisPeer)
		}
	}

	msg := &messages.TssControl{
		ReqHash:     targetHash,
		ReqKey:      key,
		RequestType: 0,
		Msg:         nil,
	}
	t.blameMgr.GetShareMgr().Set(targetHash)
	switch msgType {
	case messages.TSSKeyGenVerMsg:
		msg.RequestType = messages.TSSKeyGenMsg
		return t.processRequestMsgFromPeer(peersIDs, msg, true)
	case messages.TSSKeySignVerMsg:
		msg.RequestType = messages.TSSKeySignMsg
		return t.processRequestMsgFromPeer(peersIDs, msg, true)
	case messages.TSSKeySignMsg, messages.TSSKeyGenMsg:
		msg.RequestType = msgType
		return t.processRequestMsgFromPeer(peersIDs, msg, true)
	default:
		t.logger.Debug().Msg("unknown message type")
		return nil
	}
}

func (t *TssCommon) processVerMsg(broadcastConfirmMsg *messages.BroadcastConfirmMessage, msgType messages.THORChainTSSMessageType) error {
	t.logger.Debug().Msg("process ver msg")
	defer t.logger.Debug().Msg("finish process ver msg")
	if nil == broadcastConfirmMsg {
		return nil
	}
	partyInfo := t.getPartyInfo()
	if nil == partyInfo {
		return errors.New("can't process ver msg , local party is not ready")
	}
	key := broadcastConfirmMsg.Key
	localCacheItem := t.TryGetLocalCacheItem(key)
	if nil == localCacheItem {
		// we didn't receive the TSS Message yet
		localCacheItem = NewLocalCacheItem(nil, broadcastConfirmMsg.Hash)
		t.updateLocalUnconfirmedMessages(key, localCacheItem)
	}

	localCacheItem.UpdateConfirmList(broadcastConfirmMsg.P2PID, broadcastConfirmMsg.Hash)
	t.logger.Debug().Msgf("total confirmed parties:%+v", localCacheItem.ConfirmedList)

	threshold, err := conversion.GetThreshold(len(partyInfo.PartyIDMap))
	if err != nil {
		return err
	}
	// if we do not have the msg, we try to request from peer otherwise, we apply this share
	if localCacheItem.Msg == nil {
		return t.requestShareFromPeer(localCacheItem, threshold, key, msgType)
	}
	return t.applyShare(localCacheItem, threshold, key, msgType)
}

func (t *TssCommon) broadcastHashToPeers(key, msgHash string, peerIDs []peer.ID, msgType messages.THORChainTSSMessageType) error {
	if len(peerIDs) == 0 {
		t.logger.Error().Msg("fail to get any peer ID")
		return errors.New("fail to get any peer ID")
	}

	broadcastConfirmMsg := &messages.BroadcastConfirmMessage{
		// P2PID will be filled up by the receiver.
		P2PID: "",
		Key:   key,
		Hash:  msgHash,
	}
	buf, err := json.Marshal(broadcastConfirmMsg)
	if err != nil {
		return fmt.Errorf("fail to marshal borad cast confirm message: %w", err)
	}
	t.logger.Debug().Msg("broadcast VerMsg to all other parties")

	p2pWrappedMSg := messages.WrappedMessage{
		MessageType: msgType,
		MsgID:       t.msgID,
		Payload:     buf,
	}
	t.renderToP2P(&messages.BroadcastMsgChan{
		WrappedMessage: p2pWrappedMSg,
		PeersID:        peerIDs,
	})

	return nil
}

func (t *TssCommon) receiverBroadcastHashToPeers(wireMsg *messages.WireMessage, msgType messages.THORChainTSSMessageType) error {
	var peerIDs []peer.ID
	dataOwnerPartyID := wireMsg.Routing.From.Id
	dataOwnerPeerID, ok := t.PartyIDtoP2PID[dataOwnerPartyID]
	if !ok {
		return errors.New("error in find the data owner peerID")
	}
	t.P2PPeersLock.RLock()
	for _, el := range t.P2PPeers {
		if el == dataOwnerPeerID {
			continue
		}
		peerIDs = append(peerIDs, el)
	}
	t.P2PPeersLock.RUnlock()
	msgVerType := getBroadcastMessageType(msgType)
	key := wireMsg.GetCacheKey()
	msgHash, err := conversion.BytesToHashString(wireMsg.Message)
	if err != nil {
		return fmt.Errorf("fail to calculate hash of the wire message: %w", err)
	}
	err = t.broadcastHashToPeers(key, msgHash, peerIDs, msgVerType)
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to broadcast the hash to peers")
		return err
	}
	return nil
}

// processTSSMsg
func (t *TssCommon) processTSSMsg(wireMsg *messages.WireMessage, msgType messages.THORChainTSSMessageType, forward bool) error {
	t.logger.Debug().Msg("process wire message")
	defer t.logger.Debug().Msg("finish process wire message")

	if wireMsg == nil || wireMsg.Routing == nil || wireMsg.Routing.From == nil {
		t.logger.Warn().Msg("received msg invalid")
		return errors.New("invalid wireMsg")
	}
	partyIDMap := t.getPartyInfo().PartyIDMap
	dataOwner, ok := partyIDMap[wireMsg.Routing.From.Id]
	if !ok {
		t.logger.Error().Msg("error in find the data owner")
		return errors.New("error in find the data owner")
	}
	keyBytes := dataOwner.GetKey()
	var pk secp256k1.PubKey
	pk = keyBytes
	ok = verifySignature(pk, wireMsg.Message, wireMsg.Sig, t.msgID)
	if !ok {
		t.logger.Error().Msg("fail to verify the signature")
		return errors.New("signature verify failed")
	}

	// for the unicast message, we only update it local party
	if !wireMsg.Routing.IsBroadcast {
		t.logger.Debug().Msgf("msg from %s to %+v", wireMsg.Routing.From, wireMsg.Routing.To)
		return t.updateLocal(wireMsg)
	}

	// if not received the broadcast message , we save a copy locally , and then tell all others what we got
	if !forward {
		err := t.receiverBroadcastHashToPeers(wireMsg, msgType)
		if err != nil {
			t.logger.Error().Err(err).Msg("fail to broadcast msg to peers")
		}
	}

	partyInfo := t.getPartyInfo()
	key := wireMsg.GetCacheKey()
	msgHash, err := conversion.BytesToHashString(wireMsg.Message)
	if err != nil {
		return fmt.Errorf("fail to calculate hash of the wire message: %w", err)
	}
	localCacheItem := t.TryGetLocalCacheItem(key)
	if nil == localCacheItem {
		t.logger.Debug().Msgf("++%s doesn't exist yet,add a new one", key)
		localCacheItem = NewLocalCacheItem(wireMsg, msgHash)
		t.updateLocalUnconfirmedMessages(key, localCacheItem)
	} else {
		// this means we received the broadcast confirm message from other party first
		t.logger.Debug().Msgf("==%s exist", key)
		if localCacheItem.Msg == nil {
			t.logger.Debug().Msgf("==%s exist, set message", key)
			localCacheItem.Msg = wireMsg
			localCacheItem.Hash = msgHash
		}
	}
	localCacheItem.UpdateConfirmList(t.localPeerID, msgHash)

	threshold, err := conversion.GetThreshold(len(partyInfo.PartyIDMap))
	if err != nil {
		return err
	}
	return t.applyShare(localCacheItem, threshold, key, msgType)
}

func getBroadcastMessageType(msgType messages.THORChainTSSMessageType) messages.THORChainTSSMessageType {
	switch msgType {
	case messages.TSSKeyGenMsg:
		return messages.TSSKeyGenVerMsg
	case messages.TSSKeySignMsg:
		return messages.TSSKeySignVerMsg
	default:
		return messages.Unknown // this should not happen
	}
}

func (t *TssCommon) TryGetLocalCacheItem(key string) *LocalCacheItem {
	t.unConfirmedMsgLock.Lock()
	defer t.unConfirmedMsgLock.Unlock()
	localCacheItem, ok := t.unConfirmedMessages[key]
	if !ok {
		return nil
	}
	return localCacheItem
}

func (t *TssCommon) TryGetAllLocalCached() []*LocalCacheItem {
	var localCachedItems []*LocalCacheItem
	t.unConfirmedMsgLock.Lock()
	defer t.unConfirmedMsgLock.Unlock()
	for _, value := range t.unConfirmedMessages {
		localCachedItems = append(localCachedItems, value)
	}
	return localCachedItems
}

func (t *TssCommon) updateLocalUnconfirmedMessages(key string, cacheItem *LocalCacheItem) {
	t.unConfirmedMsgLock.Lock()
	defer t.unConfirmedMsgLock.Unlock()
	t.unConfirmedMessages[key] = cacheItem
}

func (t *TssCommon) removeKey(key string) {
	t.unConfirmedMsgLock.Lock()
	defer t.unConfirmedMsgLock.Unlock()
	delete(t.unConfirmedMessages, key)
}

func (t *TssCommon) ProcessInboundMessages(finishChan chan struct{}, wg *sync.WaitGroup) {
	t.logger.Debug().Msg("start processing inbound messages")
	defer wg.Done()
	defer t.logger.Debug().Msg("stop processing inbound messages")
	for {
		select {
		case <-finishChan:
			return
		case m, ok := <-t.TssMsg:
			if !ok {
				return
			}
			var wrappedMsg messages.WrappedMessage
			if err := json.Unmarshal(m.Payload, &wrappedMsg); nil != err {
				t.logger.Error().Err(err).Msg("fail to unmarshal wrapped message bytes")
				continue
			}

			err := t.ProcessOneMessage(&wrappedMsg, m.PeerID.String())
			if err != nil {
				t.logger.Error().Err(err).Msg("fail to process the received message")
			}

		}
	}
}
