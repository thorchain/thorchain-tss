package keysign

import (
	"encoding/base64"
	"encoding/json"
	"io/ioutil"

	"github.com/binance-chain/tss-lib/ecdsa/signing"
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/conversion"
)

type NotifierTestSuite struct{}

var _ = Suite(&NotifierTestSuite{})

func (*NotifierTestSuite) SetUpSuite(c *C) {
	conversion.SetupBech32Prefix()
}

func (NotifierTestSuite) TestNewNotifier(c *C) {
	poolPubKey := conversion.GetRandomPubKey()
	n, err := NewNotifier("", []byte("hello"), poolPubKey)
	c.Assert(err, NotNil)
	c.Assert(n, IsNil)
	n, err = NewNotifier("aasfdasdf", nil, poolPubKey)
	c.Assert(err, NotNil)
	c.Assert(n, IsNil)

	n, err = NewNotifier("hello", []byte("hello"), "")
	c.Assert(err, NotNil)
	c.Assert(n, IsNil)

	n, err = NewNotifier("hello", []byte("hello"), poolPubKey)
	c.Assert(err, IsNil)
	c.Assert(n, NotNil)
	ch := n.GetResponseChannel()
	c.Assert(ch, NotNil)
}

func (NotifierTestSuite) TestNotifierHappyPath(c *C) {
	messageToSign := "yhEwrxWuNBGnPT/L7PNnVWg7gFWNzCYTV+GuX3tKRH8="
	buf, err := base64.StdEncoding.DecodeString(messageToSign)
	c.Assert(err, IsNil)
	messageID, err := common.MsgToHashString(buf)
	c.Assert(err, IsNil)
	poolPubKey := `thorpub1addwnpepq0ul3xt882a6nm6m7uhxj4tk2n82zyu647dyevcs5yumuadn4uamqx7neak`
	n, err := NewNotifier(messageID, buf, poolPubKey)
	c.Assert(err, IsNil)
	c.Assert(n, NotNil)
	sigFile := "../test_data/signature_notify/sig1.json"
	content, err := ioutil.ReadFile(sigFile)
	c.Assert(err, IsNil)
	c.Assert(content, NotNil)
	var signature signing.SignatureData
	err = json.Unmarshal(content, &signature)
	c.Assert(err, IsNil)

	sigInvalidFile := `../test_data/signature_notify/sig_invalid.json`
	contentInvalid, err := ioutil.ReadFile(sigInvalidFile)
	c.Assert(err, IsNil)
	c.Assert(contentInvalid, NotNil)
	var sigInvalid signing.SignatureData
	c.Assert(json.Unmarshal(contentInvalid, &sigInvalid), IsNil)
	// valid keysign peer , but invalid signature we should continue to listen
	finish, err := n.ProcessSignature(&sigInvalid)
	c.Assert(err, IsNil)
	c.Assert(finish, Equals, false)
	// valid signature from a keysign peer , we should accept it and bail out
	finish, err = n.ProcessSignature(&signature)
	c.Assert(err, IsNil)
	c.Assert(finish, Equals, true)

	result := <-n.GetResponseChannel()
	c.Assert(result, NotNil)
	c.Assert(&signature == result, Equals, true)
}
