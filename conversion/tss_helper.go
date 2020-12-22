package conversion

import (
	"errors"
	"math/rand"

	"github.com/blang/semver"
	sdk "github.com/cosmos/cosmos-sdk/types"
	atypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/tendermint/tendermint/crypto/secp256k1"
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
	var pk secp256k1.PubKey
	copy(pk[:], pubKey.Bytes())
	peerID, _ := GetPeerIDFromSecp256PubKey(pk)
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

func VersionLTCheck(currentVer, expectedVer string) (bool, error) {
	c, err := semver.Make(expectedVer)
	if err != nil {
		return false, errors.New("fail to parse the expected version")
	}
	v, err := semver.Make(currentVer)
	if err != nil {
		return false, errors.New("fail to parse the current version")
	}
	return v.LT(c), nil
}
