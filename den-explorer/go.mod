module github.com/bitfsorg/den-explorer

go 1.25.6

require (
	github.com/bsv-blockchain/go-sdk v1.2.18
	github.com/bitfsorg/libbitfs-go v0.0.0
)

require (
	github.com/pkg/errors v0.9.1 // indirect
	go.etcd.io/bbolt v1.4.3 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
)

replace github.com/bitfsorg/libbitfs-go => ../libbitfs-go
