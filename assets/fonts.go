// Package assets embeds runtime assets required by the server.
package assets

import "embed"

// FontFiles contains every bundled TTF and OTF font, including subdirectories.
//
//go:embed ttf
var FontFiles embed.FS

//go:embed ttf/digital/TickingTimebomb.ttf
var OfficeRoundFont []byte

//go:embed ttf/modern/HappyBomb.ttf
var FancyClockFont []byte

//go:embed ttf/modern/Perform.ttf
var SimpleTimeFont []byte
