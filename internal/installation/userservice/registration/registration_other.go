//go:build !linux

package registration

import "sidravia/internal/productlayout"

// Lookup finds no systemd user service registration on other platforms.
func Lookup() (productlayout.Layout, bool, error) {
	return productlayout.Layout{}, false, nil
}
