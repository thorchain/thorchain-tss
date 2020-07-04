module gitlab.com/thorchain/tss/go-tss

go 1.13

require (
	github.com/binance-chain/go-sdk v1.2.1
	github.com/binance-chain/tss-lib v0.0.0-00010101000000-000000000000
	github.com/btcsuite/btcd v0.20.1-beta
	github.com/cosmos/cosmos-sdk v0.38.3
	github.com/deckarep/golang-set v1.7.1
	github.com/decred/dcrd/dcrec/secp256k1 v1.0.3
	github.com/golang/protobuf v1.4.2
	github.com/gorilla/mux v1.7.3
	github.com/ipfs/go-log v1.0.4
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
	github.com/pkg/errors v0.9.1
	github.com/rs/zerolog v1.17.2
	github.com/stretchr/testify v1.6.1
	github.com/tendermint/btcd v0.1.1
	github.com/tendermint/tendermint v0.33.3
	go.opencensus.io v0.22.3 // indirect
	golang.org/x/crypto v0.0.0-20200406173513-056763e48d71
	google.golang.org/protobuf v1.24.0
	gopkg.in/check.v1 v1.0.0-20200227125254-8fa46927fb4f
)

replace github.com/etcd-io/bbolt => go.etcd.io/bbolt v1.3.5-0.20200615073812-232d8fc87f50

replace github.com/binance-chain/go-sdk => gitlab.com/thorchain/binance-sdk v1.2.2

replace github.com/binance-chain/tss-lib => github.com/notatestuser/tss-lib v0.0.0-20200706111719-d6697c2539be
