module gitlab.com/thorchain/tss/go-tss

go 1.14

require (
	github.com/binance-chain/go-sdk v0.0.0-00010101000000-000000000000
	github.com/binance-chain/tss-lib v0.0.0-00010101000000-000000000000
	github.com/btcsuite/btcd v0.20.1-beta
	github.com/cosmos/cosmos-sdk v0.38.1
	github.com/deckarep/golang-set v1.7.1
	github.com/decred/dcrd/dcrec/secp256k1 v1.0.3
	github.com/golang/protobuf v1.4.2
	github.com/gorilla/mux v1.7.3
	github.com/ipfs/go-log v1.0.4
	github.com/libp2p/go-libp2p v0.9.6
	github.com/libp2p/go-libp2p-core v0.5.7
	github.com/libp2p/go-libp2p-discovery v0.4.0
	github.com/libp2p/go-libp2p-kad-dht v0.5.2
	github.com/libp2p/go-libp2p-peerstore v0.2.6
	github.com/libp2p/go-libp2p-quic-transport v0.4.0 // indirect
	github.com/libp2p/go-libp2p-testing v0.1.1
	github.com/magiconair/properties v1.8.1
	github.com/multiformats/go-multiaddr v0.2.2
	github.com/onsi/ginkgo v1.12.1 // indirect
	github.com/rs/zerolog v1.17.2
	github.com/stretchr/testify v1.6.1
	github.com/tendermint/btcd v0.1.1
	github.com/tendermint/tendermint v0.33.3
	golang.org/x/crypto v0.0.0-20200510223506-06a226fb4e37
	golang.org/x/lint v0.0.0-20200130185559-910be7a94367 // indirect
	golang.org/x/tools v0.0.0-20200221224223-e1da425f72fd // indirect
	google.golang.org/grpc v1.28.0-pre // indirect
	google.golang.org/protobuf v1.25.0
	gopkg.in/check.v1 v1.0.0-20190902080502-41f04d3bba15
	honnef.co/go/tools v0.0.1-2020.1.3 // indirect
)

replace github.com/binance-chain/go-sdk => gitlab.com/thorchain/binance-sdk v1.2.2

replace github.com/binance-chain/tss-lib => gitlab.com/thorchain/tss/tss-lib v0.0.0-20200723071108-d21a17ff2b2e
