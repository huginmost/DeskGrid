// Package branding owns DeskGrid's product identity and original icon assets.
package branding

import _ "embed"

const Name = "DeskGrid"

//go:embed deskgrid.png
var Icon []byte

//go:embed tray-light.png
var TrayLight []byte

//go:embed tray-dark.png
var TrayDark []byte
