package common

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	btsskeygen "github.com/binance-chain/tss-lib/ecdsa/keygen"
	btss "github.com/binance-chain/tss-lib/tss"
	coskey "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	tcrypto "github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/secp256k1"
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/blame"
	"gitlab.com/thorchain/tss/go-tss/conversion"
	"gitlab.com/thorchain/tss/go-tss/messages"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

var (
	testBlamePrivKey = "YmNiMzA2ODU1NWNjMzk3NDE1OWMwMTM3MDU0NTNjN2YwMzYzZmVhZDE5NmU3NzRhOTMwOWIxN2QyZTQ0MzdkNg=="
	testSenderPubKey = "thorpub1addwnpepqtspqyy6gk22u37ztra4hq3hdakc0w0k60sfy849mlml2vrpfr0wvm6uz09"
	testPubKeys      = [...]string{"thorpub1addwnpepqtdklw8tf3anjz7nn5fly3uvq2e67w2apn560s4smmrt9e3x52nt2svmmu3", "thorpub1addwnpepqtspqyy6gk22u37ztra4hq3hdakc0w0k60sfy849mlml2vrpfr0wvm6uz09", "thorpub1addwnpepq2ryyje5zr09lq7gqptjwnxqsy2vcdngvwd6z7yt5yjcnyj8c8cn559xe69", "thorpub1addwnpepqfjcw5l4ay5t00c32mmlky7qrppepxzdlkcwfs2fd5u73qrwna0vzag3y4j"}
	testBlamePubKeys = []string{"thorpub1addwnpepqtr5p8tllhp4xaxmu77zhqen24pmrdlnekzevshaqkyzdqljm6rejnnt02t", "thorpub1addwnpepqtspqyy6gk22u37ztra4hq3hdakc0w0k60sfy849mlml2vrpfr0wvm6uz09", "thorpub1addwnpepqga4nded5hhnwsrwmrns803w7vu9mffp9r6dz4l6smaww2l5useuq6vkttg", "thorpub1addwnpepq28hfdpu3rdgvj8skzhlm8hyt5nlwwc8pjrzvn253j86e4dujj6jsmuf25q", "thorpub1addwnpepqfuq0xc67052h288r6flp67l0ny9mg6u3sxhsrlukyfg0fe9j6q36ysd33y", "thorpub1addwnpepq0jszts80udfl4pkfk6cp93647yl6fhu6pk486uwjdz2sf94qvu0kw0t6ug", "thorpub1addwnpepqw6mmffk69n5taaqhq3wsc8mvdpsrdnx960kujeh4jwm9lj8nuyux9hz5e4", "thorpub1addwnpepq0pdhm2jatzg2vy6fyw89vs6q374zayqd5498wn8ww780grq256ygq7hhjt", "thorpub1addwnpepqggwmlgd8u9t2sx4a0styqwhzrvdhpvdww7sqwnweyrh25rjwwm9q65kx9s", "thorpub1addwnpepqtssltyjvms8pa7k4yg85lnrjqtvvr2ecr36rhm7pa4ztf55tnuzzgvegpk"}
)

func TestPackage(t *testing.T) { TestingT(t) }

type TssTestSuite struct {
	privKey tcrypto.PrivKey
}

var _ = Suite(&TssTestSuite{})

func (t *TssTestSuite) SetUpSuite(c *C) {
	InitLog("info", true, "tss_common_test")
	conversion.SetupBech32Prefix()
	priHexBytes, err := base64.StdEncoding.DecodeString(testBlamePrivKey)
	c.Assert(err, IsNil)
	rawBytes, err := hex.DecodeString(string(priHexBytes))
	c.Assert(err, IsNil)
	var priKey secp256k1.PrivKey
	priKey = rawBytes[:32]
	t.privKey = priKey
}

func (t *TssTestSuite) TestGetThreshold(c *C) {
	_, err := conversion.GetThreshold(-2)
	c.Assert(err, NotNil)
	output, err := conversion.GetThreshold(4)
	c.Assert(err, IsNil)
	c.Assert(output, Equals, 2)
	output, err = conversion.GetThreshold(9)
	c.Assert(err, IsNil)
	c.Assert(output, Equals, 5)
	output, err = conversion.GetThreshold(10)
	c.Assert(err, IsNil)
	c.Assert(output, Equals, 6)
	output, err = conversion.GetThreshold(99)
	c.Assert(err, IsNil)
	c.Assert(output, Equals, 65)
}

func (t *TssTestSuite) TestMsgToHashInt(c *C) {
	input := []byte("whatever")
	result, err := MsgToHashInt(input)
	c.Assert(err, IsNil)
	c.Assert(result, NotNil)
}

func (t *TssTestSuite) TestContains(c *C) {
	t1 := btss.PartyID{
		Index: 1,
	}
	ret := Contains(nil, &t1)
	c.Assert(ret, Equals, false)

	t2 := btss.PartyID{
		Index: 2,
	}
	t3 := btss.PartyID{
		Index: 3,
	}
	testParties := []*btss.PartyID{&t2, &t3}
	ret = Contains(testParties, &t1)
	c.Assert(ret, Equals, false)
	testParties = append(testParties, &t1)
	ret = Contains(testParties, &t1)
	c.Assert(ret, Equals, true)
	ret = Contains(testParties, nil)
	c.Assert(ret, Equals, false)
}

func (t *TssTestSuite) TestTssProcessOutCh(c *C) {
	conf := TssConfig{}
	localTestPubKeys := make([]string, len(testPubKeys))
	copy(localTestPubKeys, testPubKeys[:])
	partiesID, localPartyID, err := conversion.GetParties(localTestPubKeys, testPubKeys[0])
	c.Assert(err, IsNil)
	messageRouting := btss.MessageRouting{
		From:                    localPartyID,
		To:                      partiesID[3:],
		IsBroadcast:             true,
		IsToOldCommittee:        false,
		IsToOldAndNewCommittees: false,
	}
	testFill := []byte("TEST")
	testContent := &btsskeygen.KGRound1Message{
		Commitment: testFill,
	}
	msg := btss.NewMessageWrapper(messageRouting, testContent)
	tssMsg := btss.NewMessage(messageRouting, testContent, msg)
	tssCommonStruct := NewTssCommon("", nil, conf, "test", t.privKey)
	err = tssCommonStruct.ProcessOutCh(tssMsg, messages.TSSKeyGenMsg)
	c.Assert(err, IsNil)
}

func fabricateTssMsg(c *C, privKey tcrypto.PrivKey, partyID *btss.PartyID, roundInfo, msg, msgID string, msgType messages.THORChainTSSMessageType) *messages.WrappedMessage {
	routingInfo := btss.MessageRouting{
		From:                    partyID,
		To:                      nil,
		IsBroadcast:             true,
		IsToOldCommittee:        false,
		IsToOldAndNewCommittees: false,
	}
	var dataForSign bytes.Buffer
	dataForSign.WriteString(msg)
	dataForSign.WriteString(msgID)
	sig, err := privKey.Sign(dataForSign.Bytes())
	c.Assert(err, IsNil)
	wiredMessage := messages.WireMessage{
		Routing:   &routingInfo,
		RoundInfo: roundInfo,
		Message:   []byte(msg),
		Sig:       sig,
	}

	marshaledMsg, err := json.Marshal(wiredMessage)
	c.Assert(err, IsNil)
	wrappedMsg := messages.WrappedMessage{
		MessageType: msgType,
		Payload:     marshaledMsg,
	}
	return &wrappedMsg
}

func fabricateVerMsg(c *C, hash, hashKey string) *messages.WrappedMessage {
	broadcastConfirmMsg := &messages.BroadcastConfirmMessage{
		P2PID: "",
		Key:   hashKey,
		Hash:  hash,
	}
	marshaledMsg, err := json.Marshal(broadcastConfirmMsg)
	c.Assert(err, IsNil)
	wrappedMsg := messages.WrappedMessage{
		MessageType: messages.TSSKeyGenVerMsg,
		Payload:     marshaledMsg,
	}
	return &wrappedMsg
}

func (t *TssTestSuite) testVerMsgDuplication(c *C, privKey tcrypto.PrivKey, tssCommonStruct *TssCommon, senderID *btss.PartyID, partiesID []*btss.PartyID) {
	testMsg := "testVerMsgDuplication"
	roundInfo := "round testVerMsgDuplication"
	tssCommonStruct.msgID = "123"
	msgKey := fmt.Sprintf("%s-%s", senderID.Id, roundInfo)
	wrappedMsg := fabricateTssMsg(c, privKey, senderID, roundInfo, testMsg, tssCommonStruct.msgID, messages.TSSKeyGenMsg)
	err := tssCommonStruct.ProcessOneMessage(wrappedMsg, tssCommonStruct.PartyIDtoP2PID[partiesID[1].Id].String())
	c.Assert(err, IsNil)
	localItem := tssCommonStruct.TryGetLocalCacheItem(msgKey)
	c.Assert(localItem.ConfirmedList, HasLen, 1)
	err = tssCommonStruct.ProcessOneMessage(wrappedMsg, tssCommonStruct.PartyIDtoP2PID[partiesID[1].Id].String())
	c.Assert(err, IsNil)
	c.Assert(localItem.ConfirmedList, HasLen, 1)
}

func setupProcessVerMsgEnv(c *C, privKey tcrypto.PrivKey, keyPool []string, partyNum int) (*TssCommon, []*btss.PartyID, []*btss.PartyID) {
	conf := TssConfig{}
	tssCommonStruct := NewTssCommon("", nil, conf, "test", privKey)
	localTestPubKeys := make([]string, partyNum)
	copy(localTestPubKeys, keyPool[:partyNum])
	// for the test, we choose the first pubic key as the test instance public key
	partiesID, localPartyID, err := conversion.GetParties(localTestPubKeys, keyPool[0])
	c.Assert(err, IsNil)
	partyIDMap := conversion.SetupPartyIDMap(partiesID)
	conversion.SetupIDMaps(partyIDMap, tssCommonStruct.PartyIDtoP2PID)
	ctx := btss.NewPeerContext(partiesID)
	params := btss.NewParameters(ctx, localPartyID, len(partiesID), 2)
	outCh := make(chan btss.Message, len(partiesID))
	endCh := make(chan btsskeygen.LocalPartySaveData, len(partiesID))
	keyGenParty := btsskeygen.NewLocalParty(params, outCh, endCh)
	tssCommonStruct.SetPartyInfo(&PartyInfo{
		Party:      keyGenParty,
		PartyIDMap: partyIDMap,
	})
	err = conversion.SetupIDMaps(partyIDMap, tssCommonStruct.blameMgr.PartyIDtoP2PID)
	c.Assert(err, IsNil)
	tssCommonStruct.SetLocalPeerID("fakeID")
	err = conversion.SetupIDMaps(partyIDMap, tssCommonStruct.PartyIDtoP2PID)
	c.Assert(err, IsNil)
	tssCommonStruct.blameMgr.SetPartyInfo(keyGenParty, partyIDMap)
	peerPartiesID := append(partiesID[:localPartyID.Index], partiesID[localPartyID.Index+1:]...)
	tssCommonStruct.P2PPeers = conversion.GetPeersID(tssCommonStruct.PartyIDtoP2PID, tssCommonStruct.GetLocalPeerID())
	return tssCommonStruct, peerPartiesID, partiesID
}

func (t *TssTestSuite) testDropMsgOwner(c *C, privKey tcrypto.PrivKey, tssCommonStruct *TssCommon, senderID *btss.PartyID, peerPartiesID []*btss.PartyID) {
	testMsg := "testDropMsgOwner"
	roundInfo := "round testDropMsgOwner"
	msgHash, err := conversion.BytesToHashString([]byte(testMsg))
	c.Assert(err, IsNil)
	msgKey := fmt.Sprintf("%s-%s", senderID.Id, roundInfo)
	senderMsg := fabricateTssMsg(c, privKey, senderID, roundInfo, testMsg, "123", messages.TSSKeyGenMsg)

	senderPeer, err := conversion.GetPeerIDFromPartyID(senderID)
	c.Assert(err, IsNil)
	// you can pass any p2pID in Tss message
	err = tssCommonStruct.ProcessOneMessage(senderMsg, senderPeer.String())
	c.Assert(err, IsNil)
	localItem := tssCommonStruct.TryGetLocalCacheItem(msgKey)
	c.Assert(localItem.ConfirmedList, HasLen, 1)
	wrappedVerMsg := fabricateVerMsg(c, msgHash, msgKey)
	err = tssCommonStruct.ProcessOneMessage(wrappedVerMsg, senderPeer.String())
	c.Assert(err, Equals, blame.ErrHashCheck)
	// since we re-use the tsscommon, so we may have more than one signature
	var blameSig [][]byte
	blameNodes := tssCommonStruct.blameMgr.GetBlame().BlameNodes
	for _, el := range blameNodes {
		blameSig = append(blameSig, el.BlameSignature)
	}
	var dataForSign bytes.Buffer
	dataForSign.WriteString(testMsg)
	dataForSign.WriteString("123")
	expected, err := privKey.Sign(dataForSign.Bytes())
	c.Assert(err, IsNil)
	found := false
	for _, el := range blameSig {
		if bytes.Equal(el, expected) {
			found = true
			break
		}
	}
	c.Assert(found, Equals, true)
}

func (t *TssTestSuite) testProcessControlMsg(c *C, tssCommonStruct *TssCommon) {
	controlMsg := messages.TssControl{
		ReqHash:     "testHash",
		ReqKey:      "testKey",
		RequestType: messages.TSSKeyGenMsg,
		Msg:         nil,
	}
	payload, err := json.Marshal(controlMsg)
	c.Assert(err, IsNil)
	wrappedMsg := messages.WrappedMessage{
		MessageType: messages.TSSControlMsg,
		Payload:     payload,
	}

	err = tssCommonStruct.ProcessOneMessage(&wrappedMsg, "1")
	c.Assert(err, NotNil)
	err = tssCommonStruct.ProcessOneMessage(&wrappedMsg, "16Uiu2HAmACG5DtqmQsHtXg4G2sLS65ttv84e7MrL4kapkjfmhxAp")
	c.Assert(err, IsNil)
	tssCommonStruct.blameMgr.GetShareMgr().Set("testHash")

	msg := messages.WireMessage{
		Routing:   nil,
		RoundInfo: "",
		Message:   []byte("test"),
		Sig:       []byte("test"),
	}
	controlMsg = messages.TssControl{
		ReqHash:     "testHash",
		ReqKey:      "testKey",
		RequestType: messages.TSSKeyGenMsg,
		Msg:         &msg,
	}
	payload, err = json.Marshal(controlMsg)
	c.Assert(err, IsNil)
	wrappedMsg = messages.WrappedMessage{
		MessageType: messages.TSSControlMsg,
		Payload:     payload,
	}

	err = tssCommonStruct.ProcessOneMessage(&wrappedMsg, "16Uiu2HAmACG5DtqmQsHtXg4G2sLS65ttv84e7MrL4kapkjfmhxAp")
	c.Assert(err, ErrorMatches, "invalid wireMsg")
}

func (t *TssTestSuite) testProcessTaskDone(c *C, tssCommonStruct *TssCommon) {
	taskDone := messages.TssTaskNotifier{TaskDone: true}
	marshaledMsg, err := json.Marshal(taskDone)
	c.Assert(err, IsNil)
	wrappedMsg := messages.WrappedMessage{
		MessageType: messages.TSSTaskDone,
		Payload:     marshaledMsg,
	}
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		err = tssCommonStruct.ProcessOneMessage(&wrappedMsg, "1")
		c.Assert(err, IsNil)
		err = tssCommonStruct.ProcessOneMessage(&wrappedMsg, "2")
		c.Assert(err, IsNil)
		err = tssCommonStruct.ProcessOneMessage(&wrappedMsg, "3")
		c.Assert(err, IsNil)
	}()
	select {
	case <-tssCommonStruct.taskDone:
		return
	case <-time.After(time.Millisecond * 20):
		c.Fail()
	}
	wg.Done()
}

func (t *TssTestSuite) testVerMsgAndUpdateFromPeer(c *C, tssCommonStruct *TssCommon, senderID *btss.PartyID, partiesID []*btss.PartyID) {
	testMsg := "testVerMsgAndUpdate2"
	roundInfo := "round testVerMsgAndUpdate2"
	msgHash, err := conversion.BytesToHashString([]byte(testMsg))
	c.Assert(err, IsNil)
	msgKey := fmt.Sprintf("%s-%s", senderID.Id, roundInfo)
	// we send the verify message from the the same sender, Tss should only accept the first verify message
	wrappedVerMsg := fabricateVerMsg(c, msgHash, msgKey)
	err = tssCommonStruct.ProcessOneMessage(wrappedVerMsg, tssCommonStruct.PartyIDtoP2PID[partiesID[1].Id].String())
	c.Assert(err, IsNil)
	localItem := tssCommonStruct.TryGetLocalCacheItem(msgKey)
	c.Assert(localItem.ConfirmedList, HasLen, 1)
	err = tssCommonStruct.ProcessOneMessage(wrappedVerMsg, tssCommonStruct.PartyIDtoP2PID[partiesID[1].Id].String())
	c.Assert(err, IsNil)
	localItem = tssCommonStruct.TryGetLocalCacheItem(msgKey)
	c.Assert(localItem.ConfirmedList, HasLen, 1)
}

func (t *TssTestSuite) testVerMsgAndUpdate(c *C, tssCommonStruct *TssCommon, senderID *btss.PartyID, partiesID []*btss.PartyID) {
	testMsg := "testVerMsgAndUpdate"
	roundInfo := "round testVerMsgAndUpdate"
	msgHash, err := conversion.BytesToHashString([]byte(testMsg))
	c.Assert(err, IsNil)
	msgKey := fmt.Sprintf("%s-%s", senderID.Id, roundInfo)
	wrappedMsg := fabricateTssMsg(c, t.privKey, senderID, roundInfo, testMsg, "123", messages.TSSKeyGenMsg)
	// you can pass any p2pID in Tss message
	err = tssCommonStruct.ProcessOneMessage(wrappedMsg, tssCommonStruct.PartyIDtoP2PID[senderID.Id].String())
	c.Assert(err, IsNil)
	localItem := tssCommonStruct.TryGetLocalCacheItem(msgKey)
	c.Assert(localItem.ConfirmedList, HasLen, 1)

	// we send the verify message from the the same sender, Tss should only accept the first verify message
	wrappedVerMsg := fabricateVerMsg(c, msgHash, msgKey)
	err = tssCommonStruct.ProcessOneMessage(wrappedVerMsg, tssCommonStruct.PartyIDtoP2PID[partiesID[1].Id].String())
	c.Assert(err, NotNil)
	// workaround: when we hit this error, in this test, it indicates we accept the share.
	if !strings.Contains(err.Error(), "fail to update the message to local party: proto:") {
		c.Fatalf("error \"%v\" did not match the expected one", err.Error())
	}
}

func findSender(arr []*btss.PartyID) *btss.PartyID {
	for _, el := range arr {
		pk := coskey.PubKey{
			Key: el.GetKey()[:],
		}
		out, _ := sdk.Bech32ifyPubKey(sdk.Bech32PubKeyTypeAccPub, &pk)
		if out == testSenderPubKey {
			return el
		}
	}
	return nil
}

// TestProcessVerMessage is the tests for processing the verified message
func (t *TssTestSuite) TestProcessVerMessage(c *C) {
	tssCommonStruct, peerPartiesID, partiesID := setupProcessVerMsgEnv(c, t.privKey, testBlamePubKeys, 4)
	sender := findSender(partiesID)
	t.testVerMsgDuplication(c, t.privKey, tssCommonStruct, sender, peerPartiesID)
	t.testVerMsgAndUpdateFromPeer(c, tssCommonStruct, sender, partiesID)
	t.testDropMsgOwner(c, t.privKey, tssCommonStruct, sender, peerPartiesID)
	t.testVerMsgAndUpdate(c, tssCommonStruct, sender, partiesID)
	t.testProcessControlMsg(c, tssCommonStruct)
	t.testProcessTaskDone(c, tssCommonStruct)
}

func (t *TssTestSuite) TestTssCommon(c *C) {
	pk, err := sdk.GetPubKeyFromBech32(sdk.Bech32PubKeyTypeAccPub, "thorpub1addwnpepqtdklw8tf3anjz7nn5fly3uvq2e67w2apn560s4smmrt9e3x52nt2svmmu3")
	c.Assert(err, IsNil)
	peerID, err := conversion.GetPeerIDFromSecp256PubKey(pk.Bytes())
	c.Assert(err, IsNil)
	broadcastChannel := make(chan *messages.BroadcastMsgChan)
	sk := secp256k1.GenPrivKey()
	tssCommon := NewTssCommon(peerID.String(), broadcastChannel, TssConfig{}, "message-id", sk)
	c.Assert(tssCommon, NotNil)
	stopchan := make(chan struct{})
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		tssCommon.ProcessInboundMessages(stopchan, &wg)
	}()
	bi, err := MsgToHashInt([]byte("whatever"))
	c.Assert(err, IsNil)
	wrapMsg := fabricateTssMsg(c, sk, btss.NewPartyID("1,", "test", bi), "roundInfo", "message", "123", messages.TSSKeyGenMsg)
	buf, err := json.Marshal(wrapMsg)
	c.Assert(err, IsNil)
	pMsg := &p2p.Message{
		PeerID:  peerID,
		Payload: buf,
	}

	tssCommon.partyInfo = &PartyInfo{
		Party:      nil,
		PartyIDMap: make(map[string]*btss.PartyID),
	}
	tssCommon.TssMsg <- pMsg
	close(stopchan)
	wg.Wait()
}

func (t *TssTestSuite) TestProcessInvalidMsgBlame(c *C) {
	tssCommonStruct, peerPartiesID, partiesID := setupProcessVerMsgEnv(c, t.privKey, testBlamePubKeys, 4)
	sender := findSender(partiesID)

	testMsg := "testVerMsgDuplication"
	roundInfo := "round testMessage"
	tssCommonStruct.msgID = "123"
	wrappedMsg := fabricateTssMsg(c, t.privKey, sender, roundInfo, testMsg, tssCommonStruct.msgID, messages.TSSKeyGenMsg)

	var wiredMsg messages.WireMessage
	err := json.Unmarshal(wrappedMsg.Payload, &wiredMsg)
	c.Assert(err, IsNil)
	culprits := peerPartiesID[:3]
	for _, el := range culprits[:2] {
		key := fmt.Sprintf("%s-%s", el.Id, roundInfo)
		tssCommonStruct.blameMgr.GetRoundMgr().Set(key, &wiredMsg)
	}

	fakeErr := btss.NewError(errors.New("test error"), "test task", 1, nil, culprits...)
	tssCommonStruct.processInvalidMsgBlame(&wiredMsg, blame.RoundInfo{RoundMsg: roundInfo}, fakeErr)
	blameResult := tssCommonStruct.GetBlameMgr().GetBlame()
	c.Assert(blameResult.BlameNodes, HasLen, 3)
	for _, el := range blameResult.BlameNodes[:2] {
		c.Assert(el.BlameData, DeepEquals, []byte(testMsg))
	}
	// for the last one, since we do not store the msg before hand, it should return no record of this party
	c.Assert(blameResult.BlameNodes[2].BlameData, HasLen, 0)
}
