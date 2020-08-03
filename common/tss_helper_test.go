package common

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/tendermint/tendermint/crypto/secp256k1"
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/blame"
	"gitlab.com/thorchain/tss/go-tss/conversion"
	"gitlab.com/thorchain/tss/go-tss/messages"
)

type tssHelpSuite struct{}

var _ = Suite(&tssHelpSuite{})

func (t *tssHelpSuite) TestGetHashToBroadcast(c *C) {
	testMap := make(map[string]string)
	_, _, err := getHighestFreq(testMap)
	c.Assert(err, NotNil)
	_, _, err = getHighestFreq(nil)
	c.Assert(err, NotNil)
	testMap["1"] = "aa"
	testMap["2"] = "aa"
	testMap["3"] = "aa"
	testMap["4"] = "ab"
	testMap["5"] = "bb"
	testMap["6"] = "bb"
	testMap["7"] = "bc"
	testMap["8"] = "cd"
	val, freq, err := getHighestFreq(testMap)
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "aa")
	c.Assert(freq, Equals, 3)
}

func (t *tssHelpSuite) TestMsgSignAndVerification(c *C) {
	msg := []byte("hello")
	msgID := "123"
	sk := secp256k1.GenPrivKey()
	sig, err := generateSignature(msg, msgID, sk)
	c.Assert(err, IsNil)
	ret := verifySignature(sk.PubKey(), msg, sig, msgID)
	c.Assert(ret, Equals, true)
}

func (t *tssHelpSuite) TestMsgToHashString(c *C) {
	out, err := MsgToHashString([]byte("hello"))
	c.Assert(err, IsNil)
	c.Assert(out, Equals, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824")
	_, err = MsgToHashString(nil)
	c.Assert(err, NotNil)
}

func (t *tssHelpSuite) TestTssCommon_NotifyTaskDone(c *C) {
	conversion.SetupBech32Prefix()
	pk, err := sdk.GetPubKeyFromBech32(sdk.Bech32PubKeyTypeAccPub, "thorpub1addwnpepqtdklw8tf3anjz7nn5fly3uvq2e67w2apn560s4smmrt9e3x52nt2svmmu3")
	c.Assert(err, IsNil)
	peerID, err := conversion.GetPeerIDFromSecp256PubKey(pk.(secp256k1.PubKeySecp256k1))
	c.Assert(err, IsNil)
	sk := secp256k1.GenPrivKey()
	tssCommon := NewTssCommon(peerID.String(), nil, TssConfig{}, "message-id", sk)
	err = tssCommon.NotifyTaskDone(true, blame.Blame{})
	c.Assert(err, IsNil)
}

func (t *tssHelpSuite) TestTssCommon_processRequestMsgFromPeer(c *C) {
	pk, err := sdk.GetPubKeyFromBech32(sdk.Bech32PubKeyTypeAccPub, "thorpub1addwnpepqtdklw8tf3anjz7nn5fly3uvq2e67w2apn560s4smmrt9e3x52nt2svmmu3")
	c.Assert(err, IsNil)
	peerID, err := conversion.GetPeerIDFromSecp256PubKey(pk.(secp256k1.PubKeySecp256k1))
	c.Assert(err, IsNil)
	sk := secp256k1.GenPrivKey()
	testPeer, err := peer.Decode("16Uiu2HAm2FzqoUdS6Y9Esg2EaGcAG5rVe1r6BFNnmmQr2H3bqafa")
	c.Assert(err, IsNil)
	tssCommon := NewTssCommon(peerID.String(), nil, TssConfig{}, "message-id", sk)
	err = tssCommon.processRequestMsgFromPeer([]peer.ID{testPeer}, nil, true)
	c.Assert(err, IsNil)
	err = tssCommon.processRequestMsgFromPeer([]peer.ID{testPeer}, nil, false)
	c.Assert(err, NotNil)
	msg := messages.TssControl{
		ReqHash:     "",
		ReqKey:      "test",
		RequestType: 0,
		Msg:         nil,
	}

	tssCommon.blameMgr.GetRoundMgr().Set("test", nil)
	err = tssCommon.processRequestMsgFromPeer([]peer.ID{testPeer}, &msg, false)
	c.Assert(err, IsNil)
}
