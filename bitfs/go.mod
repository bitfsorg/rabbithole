module github.com/tongxiaofeng/bitfs

go 1.25.6

require (
	github.com/bsv-blockchain/go-sdk v1.2.18
	github.com/stretchr/testify v1.11.1
	github.com/tongxiaofeng/libbitfs v0.0.0
)

replace github.com/tongxiaofeng/libbitfs => ../libbitfs

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
