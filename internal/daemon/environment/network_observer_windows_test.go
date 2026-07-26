//go:build windows

package environment

import "testing"

func TestDecodeWindowsInterfaceClassification(t *testing.T) {
	tests := []struct {
		name  string
		flags uint8
		want  windowsInterfaceClassification
	}{
		{
			name:  "hardware only",
			flags: 1 << 0,
			want: windowsInterfaceClassification{
				hardwareBacked: true,
			},
		},
		{
			name:  "hardware plus connector",
			flags: 1<<0 | 1<<2,
			want: windowsInterfaceClassification{
				hardwareBacked:           true,
				physicalConnectorPresent: true,
			},
		},
		{
			name:  "software no flags",
			flags: 0,
			want:  windowsInterfaceClassification{},
		},
		{
			name:  "hardware plus filter",
			flags: 1<<0 | 1<<1,
			want: windowsInterfaceClassification{
				hardwareBacked:  true,
				filterInterface: true,
			},
		},
		{
			name:  "hardware plus endpoint",
			flags: 1<<0 | 1<<7,
			want: windowsInterfaceClassification{
				hardwareBacked:    true,
				endpointInterface: true,
			},
		},
		{
			name:  "unrelated status bits",
			flags: 1<<0 | 1<<2 | 1<<3 | 1<<4 | 1<<5 | 1<<6,
			want: windowsInterfaceClassification{
				hardwareBacked:           true,
				physicalConnectorPresent: true,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := decodeWindowsInterfaceClassification(test.flags); got != test.want {
				t.Fatalf("decodeWindowsInterfaceClassification(%08b) = %+v, want %+v", test.flags, got, test.want)
			}
		})
	}
}
