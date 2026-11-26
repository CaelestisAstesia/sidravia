//go:build !linux

package registration

import productlayout "sidravia/internal/installation/layout"

// Lookup finds no systemd user service registration on other platforms.
func Lookup() (productlayout.Layout, bool, error) {
	return productlayout.Layout{}, false, nil
}
