package conversion

import (
	"math/rand"

	sdk "github.com/cosmos/cosmos-sdk/types"
	atypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/tendermint/tendermint/crypto/secp256k1"

	"gitlab.com/thorchain/tss/go-tss/messages"
)

// GetRandomPubKey for test
func GetRandomPubKey() string {
	_, pubKey, _ := atypes.KeyTestPubAddr()
	bech32PubKey, _ := sdk.Bech32ifyPubKey(sdk.Bech32PubKeyTypeAccPub, pubKey)
	return bech32PubKey
}

// GetRandomPeerID for test
func GetRandomPeerID() peer.ID {
	_, pubKey, _ := atypes.KeyTestPubAddr()
	peerID, _ := GetPeerIDFromSecp256PubKey(pubKey.(secp256k1.PubKeySecp256k1))
	return peerID
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
)

func RandStringBytesMask(n int) string {
	b := make([]byte, n)
	for i := 0; i < n; {
		if idx := int(rand.Int63() & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i++
		}
	}
	return string(b)
}

func PhraseToString() map[string]string {
	phrases := make(map[string]string)
	phrases[messages.KEYGEN1] = "1"
	phrases[messages.KEYGEN2aUnicast] = "2"
	phrases[messages.KEYGEN2b] = "3"
	phrases[messages.KEYGEN3] = "4"
	return phrases
}
