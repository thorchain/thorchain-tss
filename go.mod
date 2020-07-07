module gitlab.com/thorchain/tss/go-tss

go 1.13

require (
	github.com/binance-chain/go-sdk v1.2.1
	github.com/binance-chain/tss-lib v1.3.1
	github.com/btcsuite/btcd v0.20.1-beta
	github.com/cosmos/cosmos-sdk v0.38.3
	github.com/deckarep/golang-set v1.7.1
	github.com/decred/dcrd/dcrec/secp256k1 v1.0.3
	github.com/gogo/protobuf v1.3.1
	github.com/golang/protobuf v1.3.4
	github.com/gorilla/mux v1.7.3
	github.com/ipfs/go-log v1.0.2
	github.com/jackpal/go-nat-pmp v1.0.2 // indirect
	github.com/libp2p/go-libp2p v0.5.2
	github.com/libp2p/go-libp2p-core v0.3.1
	github.com/libp2p/go-libp2p-discovery v0.2.0
	github.com/libp2p/go-libp2p-kad-dht v0.3.0
	github.com/libp2p/go-libp2p-peerstore v0.1.4
	github.com/libp2p/go-libp2p-testing v0.1.1
	github.com/libp2p/go-yamux v1.3.8 // indirect
	github.com/magiconair/properties v1.8.1
	github.com/multiformats/go-multiaddr v0.2.0
	github.com/rs/zerolog v1.17.2
	github.com/stretchr/testify v1.5.1
	github.com/tendermint/btcd v0.1.1
	github.com/tendermint/tendermint v0.33.3
	go.opencensus.io v0.22.3 // indirect
	go.uber.org/atomic v1.5.1 // indirect
	go.uber.org/multierr v1.4.0 // indirect
	go.uber.org/zap v1.14.0 // indirect
	golang.org/x/crypto v0.0.0-20200221231518-2aa609cf4a9d
	golang.org/x/lint v0.0.0-20200130185559-910be7a94367 // indirect
	golang.org/x/net v0.0.0-20200222125558-5a598a2470a0 // indirect
	golang.org/x/sys v0.0.0-20200223170610-d5e6a3e2c0ae // indirect
	golang.org/x/tools v0.0.0-20200221224223-e1da425f72fd // indirect
	google.golang.org/genproto v0.0.0-20191206224255-0243a4be9c8f // indirect
	gopkg.in/check.v1 v1.0.0-20190902080502-41f04d3bba15
	honnef.co/go/tools v0.0.1-2020.1.3 // indirect
)

replace github.com/binance-chain/go-sdk => gitlab.com/thorchain/binance-sdk v1.2.2
