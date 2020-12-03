module github.com/automsen/go-tun2socks

go 1.13

require (
	github.com/eycorsican/go-tun2socks v1.16.9
	github.com/google/gopacket v1.1.17
	github.com/miekg/dns v1.1.22
	github.com/shadowsocks/go-shadowsocks2 v0.0.11
	github.com/songgao/water v0.0.0-20190725173103-fd331bda3f4b
	golang.org/x/net v0.0.0-20191021144547-ec77196f6094
	golang.org/x/sys v0.0.0-20200302150141-5c8b2ff67527
	golang.org/x/text v0.3.2
	v2ray.com/core v4.19.1+incompatible
)

replace github.com/eycorsican/go-tun2socks => github.com/automsen/go-tun2socks v1.0.9-0.20200814044818-ee3275c43e54

replace v2ray.com/core => github.com/automsen/v2ray-core v0.0.0-20200621073531-898a5935c60d

replace github.com/songgao/water => github.com/automsen/water v0.0.0-20200621073504-499fe8e42129
