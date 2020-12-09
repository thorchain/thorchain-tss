package keysign

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tsslibcommon "github.com/binance-chain/tss-lib/common"
	btss "github.com/binance-chain/tss-lib/tss"
	"github.com/ipfs/go-log"
	"github.com/libp2p/go-libp2p-peerstore/addr"
	zlog "github.com/rs/zerolog/log"

	"gitlab.com/thorchain/tss/go-tss/conversion"

	"github.com/libp2p/go-libp2p-core/peer"
	maddr "github.com/multiformats/go-multiaddr"
	tcrypto "github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/secp256k1"
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/messages"
	"gitlab.com/thorchain/tss/go-tss/p2p"
	"gitlab.com/thorchain/tss/go-tss/storage"
)

var (
	testPubKeys = []string{
		"thorpub1addwnpepq2ryyje5zr09lq7gqptjwnxqsy2vcdngvwd6z7yt5yjcnyj8c8cn559xe69", // peerID is 16Uiu2HAm4TmEzUqy3q3Dv7HvdoSboHk5sFj2FH3npiN5vDbJC6gh
		"thorpub1addwnpepqfjcw5l4ay5t00c32mmlky7qrppepxzdlkcwfs2fd5u73qrwna0vzag3y4j", // peerID is 16Uiu2HAm2FzqoUdS6Y9Esg2EaGcAG5rVe1r6BFNnmmQr2H3bqafa
		"thorpub1addwnpepqtdklw8tf3anjz7nn5fly3uvq2e67w2apn560s4smmrt9e3x52nt2svmmu3", // peerID is 16Uiu2HAmACG5DtqmQsHtXg4G2sLS65ttv84e7MrL4kapkjfmhxAp
		"thorpub1addwnpepqtspqyy6gk22u37ztra4hq3hdakc0w0k60sfy849mlml2vrpfr0wvm6uz09", // peerID is 16Uiu2HAmAWKWf5vnpiAhfdSQebTbbB3Bg35qtyG7Hr4ce23VFA8V
	}
	testPriKeyArr = []string{
		"6LABmWB4iXqkqOJ9H0YFEA2CSSx6bA7XAKGyI/TDtas=",
		"528pkgjuCWfHx1JihEjiIXS7jfTS/viEdAbjqVvSifQ=",
		"JFB2LIJZtK+KasK00NcNil4PRJS4c4liOnK0nDalhqc=",
		"vLMGhVXMOXQVnAE3BUU8fwNj/q0ZbndKkwmxfS5EN9Y=",
	}

	testNodePrivkey = []string{
		"ZThiMDAxOTk2MDc4ODk3YWE0YThlMjdkMWY0NjA1MTAwZDgyNDkyYzdhNmMwZWQ3MDBhMWIyMjNmNGMzYjVhYg==",
		"ZTc2ZjI5OTIwOGVlMDk2N2M3Yzc1MjYyODQ0OGUyMjE3NGJiOGRmNGQyZmVmODg0NzQwNmUzYTk1YmQyODlmNA==",
		"MjQ1MDc2MmM4MjU5YjRhZjhhNmFjMmI0ZDBkNzBkOGE1ZTBmNDQ5NGI4NzM4OTYyM2E3MmI0OWMzNmE1ODZhNw==",
		"YmNiMzA2ODU1NWNjMzk3NDE1OWMwMTM3MDU0NTNjN2YwMzYzZmVhZDE5NmU3NzRhOTMwOWIxN2QyZTQ0MzdkNg==",
	}
	targets = []string{
		"16Uiu2HAmACG5DtqmQsHtXg4G2sLS65ttv84e7MrL4kapkjfmhxAp", "16Uiu2HAm4TmEzUqy3q3Dv7HvdoSboHk5sFj2FH3npiN5vDbJC6gh",
		"16Uiu2HAm2FzqoUdS6Y9Esg2EaGcAG5rVe1r6BFNnmmQr2H3bqafa",
	}
)

func TestPackage(t *testing.T) {
	TestingT(t)
}

type MockLocalStateManager struct {
	file string
}

func (m *MockLocalStateManager) SaveLocalState(state storage.KeygenLocalState) error {
	return nil
}

func (m *MockLocalStateManager) GetLocalState(pubKey string) (storage.KeygenLocalState, error) {
	buf, err := ioutil.ReadFile(m.file)
	if err != nil {
		return storage.KeygenLocalState{}, err
	}
	var state storage.KeygenLocalState
	if err := json.Unmarshal(buf, &state); err != nil {
		return storage.KeygenLocalState{}, err
	}
	return state, nil
}

func (s *MockLocalStateManager) SaveAddressBook(address map[peer.ID]addr.AddrList) error {
	return nil
}

func (s *MockLocalStateManager) RetrieveP2PAddresses() (addr.AddrList, error) {
	return nil, os.ErrNotExist
}

type TssKeysignTestSuite struct {
	comms        []*p2p.Communication
	partyNum     int
	stateMgrs    []storage.LocalStateManager
	nodePrivKeys []tcrypto.PrivKey
	targetPeers  []peer.ID
}

var _ = Suite(&TssKeysignTestSuite{})

func (s *TssKeysignTestSuite) SetUpSuite(c *C) {
	conversion.SetupBech32Prefix()
	common.InitLog("info", true, "keysign_test")

	for _, el := range testNodePrivkey {
		priHexBytes, err := base64.StdEncoding.DecodeString(el)
		c.Assert(err, IsNil)
		rawBytes, err := hex.DecodeString(string(priHexBytes))
		c.Assert(err, IsNil)
		var priKey secp256k1.PrivKey
		priKey = rawBytes[:32]
		s.nodePrivKeys = append(s.nodePrivKeys, priKey)
	}

	for _, el := range targets {
		p, err := peer.Decode(el)
		c.Assert(err, IsNil)
		s.targetPeers = append(s.targetPeers, p)
	}
}

func (s *TssKeysignTestSuite) SetUpTest(c *C) {
	if testing.Short() {
		c.Skip("skip the test")
		return
	}
	ports := []int{
		17666, 17667, 17668, 17669,
	}
	s.partyNum = 4
	s.comms = make([]*p2p.Communication, s.partyNum)
	s.stateMgrs = make([]storage.LocalStateManager, s.partyNum)
	bootstrapPeer := "/ip4/127.0.0.1/tcp/17666/p2p/16Uiu2HAm4TmEzUqy3q3Dv7HvdoSboHk5sFj2FH3npiN5vDbJC6gh"
	multiAddr, err := maddr.NewMultiaddr(bootstrapPeer)
	c.Assert(err, IsNil)
	for i := 0; i < s.partyNum; i++ {
		buf, err := base64.StdEncoding.DecodeString(testPriKeyArr[i])
		c.Assert(err, IsNil)
		if i == 0 {
			comm, err := p2p.NewCommunication("asgard", nil, ports[i], "")
			c.Assert(err, IsNil)
			c.Assert(comm.Start(buf), IsNil)
			s.comms[i] = comm
			continue
		}
		comm, err := p2p.NewCommunication("asgard", []maddr.Multiaddr{multiAddr}, ports[i], "")
		c.Assert(err, IsNil)
		c.Assert(comm.Start(buf), IsNil)
		s.comms[i] = comm
	}

	for i := 0; i < s.partyNum; i++ {
		f := &MockLocalStateManager{
			file: fmt.Sprintf("../test_data/keysign_data/%d.json", i),
		}
		s.stateMgrs[i] = f
	}
}

func (s *TssKeysignTestSuite) TestSignMessage(c *C) {
	if testing.Short() {
		c.Skip("skip the test")
		return
	}
	log.SetLogLevel("tss-lib", "info")
	sort.Strings(testPubKeys)
	req := NewRequest("thorpub1addwnpepqv6xp3fmm47dfuzglywqvpv8fdjv55zxte4a26tslcezns5czv586u2fw33", []string{"helloworld-test", "t"}, 10, testPubKeys, "")
	sort.Strings(req.Messages)
	dat := []byte(strings.Join(req.Messages, ","))
	messageID, err := common.MsgToHashString(dat)
	c.Assert(err, IsNil)
	wg := sync.WaitGroup{}
	lock := &sync.Mutex{}
	keysignResult := make(map[int][]*tsslibcommon.ECSignature)
	conf := common.TssConfig{
		KeyGenTimeout:   90 * time.Second,
		KeySignTimeout:  90 * time.Second,
		PreParamTimeout: 5 * time.Second,
	}
	var msgForSign [][]byte
	msgForSign = append(msgForSign, []byte(req.Messages[0]))
	msgForSign = append(msgForSign, []byte(req.Messages[1]))
	for i := 0; i < s.partyNum; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			comm := s.comms[idx]
			stopChan := make(chan struct{})
			keysignIns := NewTssKeySign(comm.GetLocalPeerID(),
				conf,
				comm.BroadcastMsgChan,
				stopChan, messageID,
				s.nodePrivKeys[idx], s.comms[idx], s.stateMgrs[idx], 2)
			keysignMsgChannel := keysignIns.GetTssKeySignChannels()

			comm.SetSubscribe(messages.TSSKeySignMsg, messageID, keysignMsgChannel)
			comm.SetSubscribe(messages.TSSKeySignVerMsg, messageID, keysignMsgChannel)
			comm.SetSubscribe(messages.TSSControlMsg, messageID, keysignMsgChannel)
			comm.SetSubscribe(messages.TSSTaskDone, messageID, keysignMsgChannel)
			defer comm.CancelSubscribe(messages.TSSKeySignMsg, messageID)
			defer comm.CancelSubscribe(messages.TSSKeySignVerMsg, messageID)
			defer comm.CancelSubscribe(messages.TSSControlMsg, messageID)
			defer comm.CancelSubscribe(messages.TSSTaskDone, messageID)

			localState, err := s.stateMgrs[idx].GetLocalState(req.PoolPubKey)
			c.Assert(err, IsNil)
			sig, err := keysignIns.SignMessage(msgForSign, localState, req.SignerPubKeys)

			c.Assert(err, IsNil)
			lock.Lock()
			defer lock.Unlock()
			keysignResult[idx] = sig
		}(i)
	}
	wg.Wait()

	var signatures []string
	for _, item := range keysignResult {
		if len(signatures) == 0 {
			for _, each := range item {
				signatures = append(signatures, string(each.GetSignature()))
			}
			continue
		}
		var targetSignatures []string
		for _, each := range item {
			targetSignatures = append(targetSignatures, string(each.GetSignature()))
		}

		c.Assert(signatures, DeepEquals, targetSignatures)

	}
}

func observeAndStop(c *C, tssKeySign *TssKeySign, stopChan chan struct{}) {
	for {
		select {
		case <-stopChan:
			return
		case <-time.After(time.Millisecond):
			blameMgr := tssKeySign.tssCommonStruct.GetBlameMgr()
			lastMsg := blameMgr.GetLastMsg()
			if lastMsg != nil && len(lastMsg.Type()) > 4 {
				a := lastMsg.Type()
				idx := strings.Index(a, "Round")
				start := idx + len("Round")
				round := a[start : start+1]
				roundD, err := strconv.Atoi(round)
				c.Assert(err, IsNil)
				if roundD > 4 {
					close(tssKeySign.stopChan)
				}

			}
		}
	}
}

func (s *TssKeysignTestSuite) TestSignMessageWithStop(c *C) {
	if testing.Short() {
		c.Skip("skip the test")
		return
	}
	sort.Strings(testPubKeys)
	req := NewRequest("thorpub1addwnpepqv6xp3fmm47dfuzglywqvpv8fdjv55zxte4a26tslcezns5czv586u2fw33", []string{"helloworld-test", "t"}, 10, testPubKeys, "")
	sort.Strings(req.Messages)
	dat := []byte(strings.Join(req.Messages, ","))
	messageID, err := common.MsgToHashString(dat)
	c.Assert(err, IsNil)

	wg := sync.WaitGroup{}
	conf := common.TssConfig{
		KeyGenTimeout:   10 * time.Second,
		KeySignTimeout:  20 * time.Second,
		PreParamTimeout: 5 * time.Second,
	}

	for i := 0; i < s.partyNum; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			comm := s.comms[idx]
			stopChan := make(chan struct{})
			keysignIns := NewTssKeySign(comm.GetLocalPeerID(),
				conf,
				comm.BroadcastMsgChan,
				stopChan, messageID,
				s.nodePrivKeys[idx], s.comms[idx], s.stateMgrs[idx], 2)
			keysignMsgChannel := keysignIns.GetTssKeySignChannels()

			comm.SetSubscribe(messages.TSSKeySignMsg, messageID, keysignMsgChannel)
			comm.SetSubscribe(messages.TSSKeySignVerMsg, messageID, keysignMsgChannel)
			comm.SetSubscribe(messages.TSSControlMsg, messageID, keysignMsgChannel)
			comm.SetSubscribe(messages.TSSTaskDone, messageID, keysignMsgChannel)
			defer comm.CancelSubscribe(messages.TSSKeySignMsg, messageID)
			defer comm.CancelSubscribe(messages.TSSKeySignVerMsg, messageID)
			defer comm.CancelSubscribe(messages.TSSControlMsg, messageID)
			defer comm.CancelSubscribe(messages.TSSTaskDone, messageID)

			localState, err := s.stateMgrs[idx].GetLocalState(req.PoolPubKey)
			c.Assert(err, IsNil)
			if idx == 1 {
				go observeAndStop(c, keysignIns, stopChan)
			}

			var msgsToSign [][]byte
			msgsToSign = append(msgsToSign, []byte(req.Messages[0]))
			msgsToSign = append(msgsToSign, []byte(req.Messages[1]))

			_, err = keysignIns.SignMessage(msgsToSign, localState, req.SignerPubKeys)
			c.Assert(err, NotNil)
			lastMsg := keysignIns.tssCommonStruct.GetBlameMgr().GetLastMsg()
			zlog.Info().Msgf("%s------->last message %v, broadcast? %v", keysignIns.tssCommonStruct.GetLocalPeerID(), lastMsg.Type(), lastMsg.IsBroadcast())
			// we skip the node 1 as we force it to stop
			if idx != 1 {
				blames := keysignIns.GetTssCommonStruct().GetBlameMgr().GetBlame().BlameNodes
				c.Assert(blames, HasLen, 1)
				c.Assert(blames[0].Pubkey, Equals, testPubKeys[1])
			}
		}(i)
	}
	wg.Wait()
}

func rejectSendToOnePeer(c *C, tssKeySign *TssKeySign, stopChan chan struct{}, targetPeers []peer.ID) {
	for {
		select {
		case <-stopChan:
			return
		case <-time.After(time.Millisecond):
			lastMsg := tssKeySign.tssCommonStruct.GetBlameMgr().GetLastMsg()
			if lastMsg != nil && len(lastMsg.Type()) > 6 {
				a := lastMsg.Type()
				idx := strings.Index(a, "Round")
				start := idx + len("Round")
				round := a[start : start+1]
				roundD, err := strconv.Atoi(round)
				c.Assert(err, IsNil)
				if roundD > 6 {
					tssKeySign.tssCommonStruct.P2PPeersLock.Lock()
					peersID := tssKeySign.tssCommonStruct.P2PPeers
					sort.Slice(peersID, func(i, j int) bool {
						return peersID[i].String() > peersID[j].String()
					})
					tssKeySign.tssCommonStruct.P2PPeers = targetPeers
					tssKeySign.tssCommonStruct.P2PPeersLock.Unlock()
					return
				}
			}
		}
	}
}

func (s *TssKeysignTestSuite) TestSignMessageRejectOnePeer(c *C) {
	if testing.Short() {
		c.Skip("skip the test")
		return
	}
	sort.Strings(testPubKeys)
	req := NewRequest("thorpub1addwnpepqv6xp3fmm47dfuzglywqvpv8fdjv55zxte4a26tslcezns5czv586u2fw33", []string{"helloworld-test", "t"}, 10, testPubKeys, "")
	sort.Strings(req.Messages)
	dat := []byte(strings.Join(req.Messages, ","))
	messageID, err := common.MsgToHashString(dat)
	c.Assert(err, IsNil)

	wg := sync.WaitGroup{}
	conf := common.TssConfig{
		KeyGenTimeout:   20 * time.Second,
		KeySignTimeout:  20 * time.Second,
		PreParamTimeout: 5 * time.Second,
	}
	for i := 0; i < s.partyNum; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			comm := s.comms[idx]
			stopChan := make(chan struct{})
			keysignIns := NewTssKeySign(comm.GetLocalPeerID(),
				conf,
				comm.BroadcastMsgChan,
				stopChan, messageID, s.nodePrivKeys[idx], s.comms[idx], s.stateMgrs[idx], 2)
			keysignMsgChannel := keysignIns.GetTssKeySignChannels()

			comm.SetSubscribe(messages.TSSKeySignMsg, messageID, keysignMsgChannel)
			comm.SetSubscribe(messages.TSSKeySignVerMsg, messageID, keysignMsgChannel)
			comm.SetSubscribe(messages.TSSControlMsg, messageID, keysignMsgChannel)
			comm.SetSubscribe(messages.TSSTaskDone, messageID, keysignMsgChannel)
			defer comm.CancelSubscribe(messages.TSSKeySignMsg, messageID)
			defer comm.CancelSubscribe(messages.TSSKeySignVerMsg, messageID)
			defer comm.CancelSubscribe(messages.TSSControlMsg, messageID)
			defer comm.CancelSubscribe(messages.TSSTaskDone, messageID)

			localState, err := s.stateMgrs[idx].GetLocalState(req.PoolPubKey)
			c.Assert(err, IsNil)
			if idx == 1 {
				go rejectSendToOnePeer(c, keysignIns, stopChan, s.targetPeers)
			}
			var msgsToSign [][]byte
			msgsToSign = append(msgsToSign, []byte(req.Messages[0]))
			msgsToSign = append(msgsToSign, []byte(req.Messages[1]))
			_, err = keysignIns.SignMessage(msgsToSign, localState, req.SignerPubKeys)
			lastMsg := keysignIns.tssCommonStruct.GetBlameMgr().GetLastMsg()
			zlog.Info().Msgf("%s------->last message %v, broadcast? %v", keysignIns.tssCommonStruct.GetLocalPeerID(), lastMsg.Type(), lastMsg.IsBroadcast())
			c.Assert(err, IsNil)
		}(i)
	}
	wg.Wait()
}

func (s *TssKeysignTestSuite) TearDownSuite(c *C) {
	for i, _ := range s.comms {
		tempFilePath := path.Join(os.TempDir(), strconv.Itoa(i))
		err := os.RemoveAll(tempFilePath)
		c.Assert(err, IsNil)
	}
}

func (s *TssKeysignTestSuite) TearDownTest(c *C) {
	if testing.Short() {
		c.Skip("skip the test")
		return
	}
	time.Sleep(time.Second)
	for _, item := range s.comms {
		c.Assert(item.Stop(), IsNil)
	}
}

func (s *TssKeysignTestSuite) TestCloseKeySignnotifyChannel(c *C) {
	conf := common.TssConfig{}
	keySignInstance := NewTssKeySign("", conf, nil, nil, "test", s.nodePrivKeys[0], s.comms[0], s.stateMgrs[0], 1)

	taskDone := messages.TssTaskNotifier{TaskDone: true}
	taskDoneBytes, err := json.Marshal(taskDone)
	c.Assert(err, IsNil)

	msg := &messages.WrappedMessage{
		MessageType: messages.TSSTaskDone,
		MsgID:       "test",
		Payload:     taskDoneBytes,
	}
	partyIdMap := make(map[string]*btss.PartyID)
	partyIdMap["1"] = nil
	partyIdMap["2"] = nil
	fakePartyInfo := &common.PartyInfo{
		PartyMap:   nil,
		PartyIDMap: partyIdMap,
	}
	keySignInstance.tssCommonStruct.SetPartyInfo(fakePartyInfo)
	err = keySignInstance.tssCommonStruct.ProcessOneMessage(msg, "node1")
	c.Assert(err, IsNil)
	err = keySignInstance.tssCommonStruct.ProcessOneMessage(msg, "node2")
	c.Assert(err, IsNil)
	err = keySignInstance.tssCommonStruct.ProcessOneMessage(msg, "node1")
	c.Assert(err, ErrorMatches, "duplicated notification from peer node1 ignored")
}
