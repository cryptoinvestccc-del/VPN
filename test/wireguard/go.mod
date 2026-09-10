// Separate module on purpose: this integration test runs a real WireGuard
// implementation in userspace, which pulls in gVisor's network stack. The
// production module stays free of that dependency — a VPN's shipped
// binaries should carry the smallest supply chain they can.
module github.com/cryptoinvestccc-del/vpn/test/wireguard

go 1.26.0

require (
	github.com/cryptoinvestccc-del/vpn v0.0.0
	golang.org/x/crypto v0.57.0
	golang.zx2c4.com/wireguard v0.0.0-20260522210424-ecfc5a8d5446
)

require (
	github.com/google/btree v1.1.2 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/time v0.7.0 // indirect
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gvisor.dev/gvisor v0.0.0-20250503011706-39ed1f5ac29c // indirect
)

replace github.com/cryptoinvestccc-del/vpn => ../..
