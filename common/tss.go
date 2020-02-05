package common

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/binance-chain/go-sdk/common/types"
	"github.com/binance-chain/tss-lib/crypto"
	btss "github.com/binance-chain/tss-lib/tss"
	"github.com/btcsuite/btcd/btcec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	cryptokey "github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/secp256k1"

	"gitlab.com/thorchain/tss/go-tss/p2p"
)

// PartyInfo the information used by tss key gen and key sign
type PartyInfo struct {
	PartyMap   map[string]btss.Party
	PartyIDMap map[string]*btss.PartyID
}

type BulkWireMsg struct {
	WiredMsg      []byte
	MsgIdentifier string
	Routing       *btss.MessageRouting
}

func NewCachedWireMsg(msg []byte, id string, r *btss.MessageRouting) BulkWireMsg {
	return BulkWireMsg{
		WiredMsg:      msg,
		MsgIdentifier: id,
		Routing:       r,
	}
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
	broadcastChannel            chan *p2p.BroadcastMsgChan
	TssMsg                      chan *p2p.Message
	P2PPeers                    []peer.ID //most of tss message are broadcast, we store the peers ID to avoid iterating
	BlamePeers                  Blame
	cachedWireBroadcastMsgLists map[string][]BulkWireMsg
	cachedWireUnicastMsgLists   map[string][]BulkWireMsg
}

func NewTssCommon(peerID string, broadcastChannel chan *p2p.BroadcastMsgChan, conf TssConfig) *TssCommon {
	return &TssCommon{
		conf:                        conf,
		logger:                      log.With().Str("module", "tsscommon").Logger(),
		partyLock:                   &sync.Mutex{},
		partyInfo:                   nil,
		PartyIDtoP2PID:              make(map[string]peer.ID),
		unConfirmedMsgLock:          &sync.Mutex{},
		unConfirmedMessages:         make(map[string]*LocalCacheItem),
		localPeerID:                 peerID,
		broadcastChannel:            broadcastChannel,
		TssMsg:                      make(chan *p2p.Message),
		P2PPeers:                    nil,
		BlamePeers:                  NewBlame(),
		cachedWireBroadcastMsgLists: make(map[string][]BulkWireMsg),
		cachedWireUnicastMsgLists:   make(map[string][]BulkWireMsg),
	}
}

func GetPriKey(priKeyString string) (cryptokey.PrivKey, error) {
	priHexBytes, err := base64.StdEncoding.DecodeString(priKeyString)
	if nil != err {
		return nil, fmt.Errorf("fail to decode private key: %w", err)
	}
	rawBytes, err := hex.DecodeString(string(priHexBytes))
	if nil != err {
		return nil, fmt.Errorf("fail to hex decode private key: %w", err)
	}
	var keyBytesArray [32]byte
	copy(keyBytesArray[:], rawBytes[:32])
	priKey := secp256k1.PrivKeySecp256k1(keyBytesArray)
	return priKey, nil
}

func GetPriKeyRawBytes(priKey cryptokey.PrivKey) ([]byte, error) {
	var keyBytesArray [32]byte
	pk, ok := priKey.(secp256k1.PrivKeySecp256k1)
	if !ok {
		return nil, errors.New("private key is not secp256p1.PrivKeySecp256k1")
	}
	copy(keyBytesArray[:], pk[:])
	return keyBytesArray[:], nil
}

func GetParties(keys []string, localPartyKey string) ([]*btss.PartyID, *btss.PartyID, error) {
	var localPartyID *btss.PartyID
	var unSortedPartiesID []*btss.PartyID
	sort.Strings(keys)
	for idx, item := range keys {
		pk, err := sdk.GetAccPubKeyBech32(item)
		if nil != err {
			return nil, nil, fmt.Errorf("fail to get account pub key address(%s): %w", item, err)
		}
		secpPk := pk.(secp256k1.PubKeySecp256k1)
		key := new(big.Int).SetBytes(secpPk[:])
		// Set up the parameters
		// Note: The `id` and `moniker` fields are for convenience to allow you to easily track participants.
		// The `id` should be a unique string representing this party in the network and `moniker` can be anything (even left blank).
		// The `uniqueKey` is a unique identifying key for this peer (such as its p2p public key) as a big.Int.
		partyID := btss.NewPartyID(strconv.Itoa(idx), "", key)
		if item == localPartyKey {
			localPartyID = partyID
		}
		unSortedPartiesID = append(unSortedPartiesID, partyID)
	}
	if localPartyID == nil {
		return nil, nil, errors.New("local party is not in the list")
	}

	partiesID := btss.SortPartyIDs(unSortedPartiesID)

	return partiesID, localPartyID, nil
}

func (t *TssCommon) renderToP2P(broadcastMsg *p2p.BroadcastMsgChan) {
	if t.broadcastChannel == nil {
		t.logger.Warn().Msg("broadcast channel is not set")
		return
	}
	t.broadcastChannel <- broadcastMsg
}

func (t *TssCommon) sendMsg(message p2p.WrappedMessage, peerIDs []peer.ID) {

	t.renderToP2P(&p2p.BroadcastMsgChan{
		WrappedMessage: message,
		PeersID:        peerIDs,
	})
}

//signers sync function
func (t *TssCommon) NodeSync(msgChan chan *p2p.Message, messageType p2p.THORChainTSSMessageType) ([]string, error) {
	var err error
	var standbyPeers []string
	peersMap := make(map[string]bool)

	peerIDs := t.P2PPeers
	if len(peerIDs) == 0 {
		t.logger.Error().Msg("fail to get any peer")
		return standbyPeers, errors.New("fail to get any peer")
	}
	wrappedMsg := p2p.WrappedMessage{
		MessageType: messageType,
		Payload:     []byte{0},
	}
	stopChan := make(chan bool, len(peerIDs))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t.sendMsg(wrappedMsg, t.P2PPeers)
		i := 0
		for {
			select {
			case <-stopChan:
				return
			case <-time.After(time.Millisecond * 500):
				t.sendMsg(wrappedMsg, t.P2PPeers)
				i += 1
			}
			if i > t.conf.SyncRetry {
				err = errors.New("too many errors in retry")
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case m := <-msgChan:
				peersMap[m.PeerID.String()] = true
				if len(peersMap) == len(peerIDs) {
					stopChan <- true
					// we send the last sync msg before we quit
					t.sendMsg(wrappedMsg, peerIDs)
					return
				}
			case <-time.After(t.conf.SyncTimeout):
				stopChan <- true
				err = ErrNodeSync
				return
			}
		}
	}()
	wg.Wait()
	for k := range peersMap {
		standbyPeers = append(standbyPeers, k)
	}
	return standbyPeers, err
}

func getPeerIDFromPartyID(partyID *btss.PartyID) (peer.ID, error) {
	pkBytes := partyID.KeyInt().Bytes()
	var pk secp256k1.PubKeySecp256k1
	copy(pk[:], pkBytes)
	return GetPeerIDFromSecp256PubKey(pk)
}

// GetConf get current configuration for Tss
func (t *TssCommon) GetConf() TssConfig {
	return t.conf
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

func BytesToHashString(msg []byte) (string, error) {
	h := sha256.New()
	_, err := h.Write(msg)
	if nil != err {
		return "", fmt.Errorf("fail to caculate sha256 hash: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// updateLocal will apply the wireMsg to local keygen/keysign party
func (t *TssCommon) updateLocal(wireMsg *p2p.WireMessage) error {
	if nil == wireMsg {
		t.logger.Warn().Msg("wire msg is nil")
	}
	partyInfo := t.getPartyInfo()
	if partyInfo == nil {
		return nil
	}
	var BulkMsg []BulkWireMsg
	json.Unmarshal(wireMsg.Message, &BulkMsg)

	for _, el := range BulkMsg {
		fmt.Printf(">>>>BBBBBBBBBBB%v>>>>>>###>>>>>>>%v\n", el.MsgIdentifier, wireMsg.RoundInfo)

		Party, ok := partyInfo.PartyMap[el.MsgIdentifier]
		if !ok {
			t.logger.Error().Msg("cannot find the party to this wired msg")
			return errors.New("cannot find the party")
		}
		partyID, ok := partyInfo.PartyIDMap[el.Routing.From.Id]
		thisParty := Party
		fmt.Printf(">>>>>>>>>>>>>>ENTER><>>>>>>>>>>>>>>>>>>yy----%v>>>>>>>>>>>from:%v----to:%v---broadcast:%v\n", el.MsgIdentifier, el.Routing.From, el.Routing.To, el.Routing.IsBroadcast)
		if _, err := thisParty.UpdateFromBytes(el.WiredMsg, partyID, wireMsg.Routing.IsBroadcast); nil != err {
			fmt.Println("WWWWWWWWWWWWCCCCCCCCCCCCCNNNNNNNNNNMMMMMMMMDDDDDDDDDDBBBBBBBBBBB")
			return fmt.Errorf("fail to set bytes to local party: %w", err)
		}
		fmt.Println(">>>>>>>>>>>>>>EXIT><>>>>>>>>>>>>>>>>>>yy")

	}

	//partyID, ok := partyInfo.PartyIDMap[wireMsg.Routing.From.Id]
	//if !ok {
	//	return fmt.Errorf("get message from unknown party %s", partyID.Id)
	//}
	//var thisParty btss.Party
	//for _, el := range partyInfo.PartyMap {
	//	thisParty = *el
	//}
	//if _, err := thisParty.UpdateFromBytes(wireMsg.Message, partyID, wireMsg.Routing.IsBroadcast); nil != err {
	//	return fmt.Errorf("fail to set bytes to local party: %w", err)
	//}
	return nil
}

func (t *TssCommon) isLocalPartyReady() bool {
	partyInfo := t.getPartyInfo()
	if nil == partyInfo {
		return false
	}
	return true
}

func (t *TssCommon) checkDupAndUpdateVerMsg(bMsg *p2p.BroadcastConfirmMessage, peerID string) bool {
	localCacheItem := t.TryGetLocalCacheItem(bMsg.Key)
	//we check whether this node has already sent the VerMsg message to avoid eclipse of others VerMsg
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

func (t *TssCommon) ProcessOneMessage(wrappedMsg *p2p.WrappedMessage, peerID string) error {
	t.logger.Debug().Msg("start process one message")
	defer t.logger.Debug().Msg("finish processing one message")
	if nil == wrappedMsg {
		return errors.New("invalid wireMessage")
	}

	switch wrappedMsg.MessageType {
	case p2p.TSSKeyGenMsg, p2p.TSSKeySignMsg:
		var wireMsg p2p.WireMessage
		if err := json.Unmarshal(wrappedMsg.Payload, &wireMsg); nil != err {
			return fmt.Errorf("fail to unmarshal wire message: %w", err)
		}
		return t.processTSSMsg(&wireMsg, wrappedMsg.MessageType)
	case p2p.TSSKeyGenVerMsg, p2p.TSSKeySignVerMsg:
		var bMsg p2p.BroadcastConfirmMessage
		if err := json.Unmarshal(wrappedMsg.Payload, &bMsg); nil != err {
			return errors.New("fail to unmarshal broadcast confirm message")
		}
		//we check whether this peer has already send us the VerMsg before update
		ret := t.checkDupAndUpdateVerMsg(&bMsg, peerID)
		fmt.Printf("2222222222222222222222222>>>>>>>>>>>%v, from peer %v\n", ret, peerID)
		if ret {
			fmt.Println("3333333333333333333333")
			return t.processVerMsg(&bMsg)
		}
		return nil
	}
	return nil
}

func (t *TssCommon) hashCheck(localCacheItem *LocalCacheItem) error {
	dataOwner := localCacheItem.Msg.Routing.From
	dataOwnerP2PID, ok := t.PartyIDtoP2PID[dataOwner.Id]
	if !ok {
		t.logger.Warn().Msgf("error in find the data Owner P2PID\n")
		return errors.New("error in find the data Owner P2PID")
	}
	localCacheItem.lock.Lock()
	defer localCacheItem.lock.Unlock()

	targetHashValue := localCacheItem.Hash
	for P2PID, hashValue := range localCacheItem.ConfirmedList {
		if P2PID == dataOwnerP2PID.String() {
			t.logger.Warn().Msgf("we detect that the data owner try to send the hash for his own message\n")
			delete(localCacheItem.ConfirmedList, P2PID)
			return ErrHashFromOwner
		}
		if targetHashValue == hashValue {
			continue
		}
		t.logger.Error().Msgf("hash is not in consistency!!")
		return ErrHashFromPeer
	}
	return nil
}

func (t *TssCommon) sendBulkMsg(wiredMsgType string, TssMsgType p2p.THORChainTSSMessageType, wiredMsgList []BulkWireMsg) error {
	// since all the messages in the list is the same round, so it must have the same dest
	// we just need to get the routing info of the first message
	//todo fix me here
	r := wiredMsgList[0].Routing

	buf, err := json.Marshal(wiredMsgList)
	if err != nil {
		return fmt.Errorf("error in marshal the cachedWireMsg: %w", err)
	}
	wireMsg := p2p.WireMessage{
		Routing:   r,
		RoundInfo: wiredMsgType,
		Message:   buf,
	}
	wireMsgBytes, err := json.Marshal(wireMsg)
	if nil != err {
		return fmt.Errorf("fail to convert tss msg to wire bytes: %w", err)
	}
	wrappedMsg := p2p.WrappedMessage{
		MessageType: TssMsgType,
		Payload:     wireMsgBytes,
	}
	peerIDs := make([]peer.ID, 0)
	if len(r.To) == 0 {
		peerIDs = t.P2PPeers
		if len(peerIDs) == 0 {
			t.logger.Error().Msg("fail to get any peer ids")
			return errors.New("fail to get any peer id")
		}

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
	t.renderToP2P(&p2p.BroadcastMsgChan{
		WrappedMessage: wrappedMsg,
		PeersID:        peerIDs,
	})

	return nil
}

func (t *TssCommon) ProcessOutCh(msg btss.Message, tssMsgType p2p.THORChainTSSMessageType) error {
	msgDat, r, err := msg.WireBytes()
	// if we cannot get the wire share, the tss keygen will fail, we just quit.
	if nil != err {
		return fmt.Errorf("fail to get wire bytes: %w", err)
	}

	if r.IsBroadcast{

		cachedWiredMsg := NewCachedWireMsg(msgDat, msg.GetFrom().Moniker, r)

		// now we store this message in cache
		cachedList, ok := t.cachedWireBroadcastMsgLists[msg.Type()]
		if !ok {
			l := []BulkWireMsg{cachedWiredMsg}
			t.cachedWireBroadcastMsgLists[msg.Type()] = l
		} else {
			cachedList = append(cachedList, cachedWiredMsg)
			t.cachedWireBroadcastMsgLists[msg.Type()] = cachedList
		}


	} else{
		cachedWiredMsg := NewCachedWireMsg(msgDat, msg.GetFrom().Moniker, r)

		fmt.Printf("YYYYYYYYYYYYYUUUUUUUUUBBBBBBBB>>>>>>%v----------%v\n",r, r.To[0].String())
		cachedList, ok := t.cachedWireBroadcastMsgLists[r.To[0].String()]
		if !ok {
			l := []BulkWireMsg{cachedWiredMsg}
			t.cachedWireBroadcastMsgLists[r.To[0].String()] = l
		} else {
			cachedList = append(cachedList, cachedWiredMsg)
			t.cachedWireUnicastMsgLists[r.To[0].String()] = cachedList
		}

	}


	// now we send the messages that have all the signers ready
	for wiredMsgType, wiredMsgList := range t.cachedWireUnicastMsgLists{
		if len(wiredMsgList) == len(t.partyInfo.PartyMap) {
			t.sendBulkMsg(wiredMsgType, tssMsgType, wiredMsgList)
			// we do need to delete this message
			delete(t.cachedWireUnicastMsgLists, wiredMsgType)
		}
	}

	// now we send the messages that have all the signers ready
	for wiredMsgType, wiredMsgList := range t.cachedWireBroadcastMsgLists{
		if len(wiredMsgList) == len(t.partyInfo.PartyMap) {
			t.sendBulkMsg(wiredMsgType, tssMsgType, wiredMsgList)
			// we do need to delete this message
			delete(t.cachedWireBroadcastMsgLists, wiredMsgType)
		}
	}

	return nil
}

func (t *TssCommon) processVerMsg(broadcastConfirmMsg *p2p.BroadcastConfirmMessage) error {
	t.logger.Debug().Msg("process ver msg")
	defer t.logger.Debug().Msg("finish process ver msg")
	if nil == broadcastConfirmMsg {
		return nil
	}
	fmt.Println("5444443333333333333")
	partyInfo := t.getPartyInfo()
	if nil == partyInfo {
		return errors.New("can't process ver msg , local party is not ready")
	}
	fmt.Println("888777766")
	key := broadcastConfirmMsg.Key
	localCacheItem := t.TryGetLocalCacheItem(key)
	if nil == localCacheItem {
		// we didn't receive the TSS Message yet
		localCacheItem = NewLocalCacheItem(nil, broadcastConfirmMsg.Hash)
		t.updateLocalUnconfirmedMessages(key, localCacheItem)
	}
	fmt.Printf("77777777777>%v\n", broadcastConfirmMsg)
	localCacheItem.UpdateConfirmList(broadcastConfirmMsg.P2PID, broadcastConfirmMsg.Hash)
	t.logger.Debug().Msgf("total confirmed parties:%+v", localCacheItem.ConfirmedList)
	fmt.Printf("total confirmed %v, party num=====%v\n", localCacheItem.TotalConfirmParty(), len(partyInfo.PartyIDMap)-1)
	//if localCacheItem.Msg == nil{
	//	fmt.Println("55555555555555NNNNNNNNNNNN")
	//}else{
	//	fmt.Printf("55555UYYYYYYYYYYY\n")
	//}
	if localCacheItem.TotalConfirmParty() == (len(partyInfo.PartyIDMap)-1) && localCacheItem.Msg != nil {
		errHashCheck := t.hashCheck(localCacheItem)

		if errHashCheck != nil {
			blamePeers, err := t.getHashCheckBlamePeers(localCacheItem, errHashCheck)
			if err != nil {
				t.logger.Error().Err(err).Msgf("error in get the blame nodes")
				t.BlamePeers.SetBlame(BlameHashCheck, nil)
				return fmt.Errorf("error in getting the blame nodes %w", errHashCheck)
			}
			blamePubKeys, _, err := t.GetBlamePubKeysLists(blamePeers)
			if err != nil {
				t.logger.Error().Err(err).Msg("fail to get the blame nodes public key")

				t.BlamePeers.SetBlame(BlameHashCheck, nil)
				return fmt.Errorf("fail to get the blame nodes public key %w", errHashCheck)
			}
			t.BlamePeers.SetBlame(BlameHashCheck, blamePubKeys)
			t.logger.Error().Msg("The consistency check failed")
			return errHashCheck
		}
		fmt.Println("1234123451234523423423423LLLL")
		if err := t.updateLocal(localCacheItem.Msg); nil != err {
			return fmt.Errorf("fail to update the message to local party: %w", err)
		}
		// the information had been confirmed by all party , we don't need it anymore
		t.logger.Debug().Msgf("remove key: %s", key)
		t.removeKey(key)
	}
	return nil
}

// processTSSMsg
func (t *TssCommon) processTSSMsg(wireMsg *p2p.WireMessage, msgType p2p.THORChainTSSMessageType) error {
	t.logger.Debug().Msg("process wire message")
	defer t.logger.Debug().Msg("finish process wire message")
	// we only update it local party
	if !wireMsg.Routing.IsBroadcast {
		t.logger.Debug().Msgf("msg from %s to %+v", wireMsg.Routing.From, wireMsg.Routing.To)
		return t.updateLocal(wireMsg)
	}
	// broadcast message , we save a copy locally , and then tell all others what we got
	msgHash, err := BytesToHashString(wireMsg.Message)
	if nil != err {
		return fmt.Errorf("fail to calculate hash of the wire message: %w", err)
	}
	partyInfo := t.getPartyInfo()
	key := wireMsg.GetCacheKey()
	//P2PID will be filled up by the receiver.
	broadcastConfirmMsg := &p2p.BroadcastConfirmMessage{
		P2PID: "",
		Key:   key,
		Hash:  msgHash,
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
	if localCacheItem.TotalConfirmParty() == (len(partyInfo.PartyIDMap) - 1) {
		fmt.Println("444444444466666666677777777777888888888888")
		if err := t.updateLocal(localCacheItem.Msg); nil != err {
			return fmt.Errorf("fail to update the message to local party: %w", err)
		}
	}
	buf, err := json.Marshal(broadcastConfirmMsg)
	if nil != err {
		return fmt.Errorf("fail to marshal borad cast confirm message: %w", err)
	}
	t.logger.Debug().Msg("broadcast VerMsg to all other parties")
	peerIDs := t.P2PPeers
	if len(peerIDs) == 0 {
		t.logger.Error().Err(err).Msg("fail to get any peer ID")
		return errors.New("fail to get any peer ID")
	}

	p2prappedMSg := p2p.WrappedMessage{
		MessageType: getBroadcastMessageType(msgType),
		Payload:     buf,
	}

	t.renderToP2P(&p2p.BroadcastMsgChan{
		WrappedMessage: p2prappedMSg,
		PeersID:        peerIDs,
	})
	return nil
}
func getBroadcastMessageType(msgType p2p.THORChainTSSMessageType) p2p.THORChainTSSMessageType {
	switch msgType {
	case p2p.TSSKeyGenMsg:
		return p2p.TSSKeyGenVerMsg
	case p2p.TSSKeySignMsg:
		return p2p.TSSKeySignVerMsg
	default:
		return p2p.Unknown // this should not happen
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

func GetTssPubKey(pubKeyPoint *crypto.ECPoint) (string, types.AccAddress, error) {
	if pubKeyPoint == nil {
		return "", types.AccAddress{}, errors.New("invalid points")
	}
	tssPubKey := btcec.PublicKey{
		Curve: btcec.S256(),
		X:     pubKeyPoint.X(),
		Y:     pubKeyPoint.Y(),
	}
	var pubKeyCompressed secp256k1.PubKeySecp256k1
	copy(pubKeyCompressed[:], tssPubKey.SerializeCompressed())
	pubKey, err := sdk.Bech32ifyAccPub(pubKeyCompressed)
	addr := types.AccAddress(pubKeyCompressed.Address().Bytes())
	return pubKey, addr, err
}
