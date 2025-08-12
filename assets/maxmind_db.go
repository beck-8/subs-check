package assets

import (
	_ "embed"

)

//go:embed GeoLite2-Country.mmdb.zst
var EmbeddedMaxMindDB []byte