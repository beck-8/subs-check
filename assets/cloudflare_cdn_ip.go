package assets

import (
	_ "embed"

)

//go:embed cloudflare_cdn_ipv4.txt
var embeddedIPv4 string

//go:embed cloudflare_cdn_ipv6.txt
var embeddedIPv6 string
