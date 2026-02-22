module github.com/tongxiaofeng/den

go 1.25.6

require github.com/tongxiaofeng/libbitfs v0.0.0

require (
	go.etcd.io/bbolt v1.4.3 // indirect
	golang.org/x/sys v0.41.0 // indirect
)

replace github.com/tongxiaofeng/libbitfs => ../libbitfs
