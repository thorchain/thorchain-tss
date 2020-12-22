package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"

	"github.com/binance-chain/tss-lib/ecdsa/keygen"
	"github.com/btcsuite/btcd/btcec"
	coskey "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type (
	KeygenLocalState struct {
		PubKey          string                    `json:"pub_key"`
		LocalData       keygen.LocalPartySaveData `json:"local_data"`
		ParticipantKeys []string                  `json:"participant_keys"` // the paticipant of last key gen
		LocalPartyKey   string                    `json:"local_party_key"`
	}
)

func getTssSecretFile(file string) (KeygenLocalState, error) {
	_, err := os.Stat(file)
	if err != nil {
		return KeygenLocalState{}, err
	}
	buf, err := ioutil.ReadFile(file)
	if err != nil {
		return KeygenLocalState{}, fmt.Errorf("file to read from file(%s): %w", file, err)
	}
	var localState KeygenLocalState
	if err := json.Unmarshal(buf, &localState); nil != err {
		return KeygenLocalState{}, fmt.Errorf("fail to unmarshal KeygenLocalState: %w", err)
	}
	return localState, nil
}

func setupBech32Prefix() {
	config := sdk.GetConfig()
	// thorchain will import go-tss as a library , thus this is not needed, we copy the prefix here to avoid go-tss to import thorchain
	config.SetBech32PrefixForAccount("thor", "thorpub")
	config.SetBech32PrefixForValidator("thorv", "thorvpub")
	config.SetBech32PrefixForConsensusNode("thorc", "thorcpub")
}

func getTssPubKey(x, y *big.Int) (string, sdk.AccAddress, error) {
	if x == nil || y == nil {
		return "", sdk.AccAddress{}, errors.New("invalid points")
	}
	tssPubKey := btcec.PublicKey{
		Curve: btcec.S256(),
		X:     x,
		Y:     y,
	}
	pubKeyCompressed := coskey.PubKey{
		Key: tssPubKey.SerializeCompressed(),
	}

	pubKey, err := sdk.Bech32ifyPubKey(sdk.Bech32PubKeyTypeAccPub, &pubKeyCompressed)
	addr := sdk.AccAddress(pubKeyCompressed.Address().Bytes())
	return pubKey, addr, err
}

func aesCTRXOR(key, inText, iv []byte) ([]byte, error) {
	// AES-128 is selected due to size of encryptKey.
	aesBlock, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	stream := cipher.NewCTR(aesBlock, iv)
	outText := make([]byte, len(inText))
	stream.XORKeyStream(outText, inText)
	return outText, err
}

func generateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	// Note that err == nil only if we read len(b) bytes.
	if err != nil {
		return nil, err
	}
	return b, nil
}
