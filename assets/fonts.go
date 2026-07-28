// Package assets embeds runtime assets required by the server.
package assets

import _ "embed"

//go:embed ttf/fixed/Pixel_ModeX.otf
var TechnicalInfoFont []byte
